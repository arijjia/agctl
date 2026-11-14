// agctl — один бинарь: многопользовательская платформа агентских рабочих
// мест (dsh web в контейнере + персональная git-репка + бэклоги).
//
// Команды:
//
//	agctl serve     — поднять UI+API+provision (демо-режим «одной кнопки»)
//	agctl migrate   — накатить миграции SQLite вручную
//	agctl backlog … — CLI ведения бэклогов (формат v1, gitctl-совместимый)
//	agctl version   — версия и сборка
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/arijjia/agctl/internal/backlog"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
	"github.com/arijjia/agctl/web"
)

// version подставляется ldflags при сборке (make/goreleaser).
var version = "dev"

// commit и buildDate — коммит и дата сборки (ldflags, make).
var (
	commit    = ""
	buildDate = ""
)

// versionLine — строка `agctl version`.
func versionLine() string {
	v := "agctl " + version
	if commit != "" {
		v += " " + commit
	}
	if buildDate != "" {
		v += " " + buildDate
	}
	return fmt.Sprintf("%s (%s/%s %s)", v, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// Дефолтов LLM в коде больше нет: платформа универсальна, а организация
// задаётся профилем (internal/profile). Нейтральный agctl — без LLM-дефолтов;
// профиль организации (например utmn) несёт свои.

// prescanFlag достаёт значение флага из argv до штатного разбора (нужен,
// чтобы дефолты остальных флагов зависели от профиля). Поддерживает
// --name=v и --name v.
// prescanFlag достаёт значение флага из argv до штатного разбора flag:
// дефолты остальных флагов зависят от профиля, который задаётся этими же
// флагами. Понимает формы --name=v, --name v и -name v.
func prescanFlag(args []string, name, def string) string {
	prefix := "--" + name + "="
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, prefix):
			return strings.TrimSpace(strings.TrimPrefix(a, prefix))
		case a == "--"+name && i+1 < len(args):
			return strings.TrimSpace(args[i+1])
		case a == "-"+name && i+1 < len(args):
			return strings.TrimSpace(args[i+1])
		}
	}
	return def
}

// main — диспетчер подкоманд CLI: serve, migrate, init-chat-home,
// seed-demo-users, backlog, connect, local, sync, version.
// Неизвестная команда печатает usage и завершает процесс кодом 2.
func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "init-chat-home": // внутренний initContainer helper для K8s Chat PVC
		cmdInitChatHome(os.Args[2:])
	case "ax-runner": // PID 1 песочницы Google AX / Agent Substrate (образ agctl-dsh)
		cmdAXRunner(os.Args[2:])
	case "seed-demo-users":
		cmdSeedDemoUsers(os.Args[2:])
	case "backlog":
		os.Exit(backlog.Main(os.Args[2:], os.Stdout, os.Stderr))
	case "completion":
		cmdCompletion(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	case "connect":
		cmdConnect(os.Args[2:])
	case "local":
		if len(os.Args) >= 3 && os.Args[2] == "connect" {
			cmdLocalConnect(os.Args[3:])
		} else {
			fmt.Fprintln(os.Stderr, "использование: agctl local connect --server URL --grant TOKEN [--launch-dsh] ...")
			os.Exit(2)
		}
	case "sync":
		cmdSync(os.Args[2:])
	case "dev": // локальный режим: платформа на машине разработчика
		cmdDev(os.Args[2:])
	case "bridge": // клиент моста к хосту (внутри контейнера локального режима)
		cmdBridge(os.Args[2:])
	case "version":
		fmt.Println(versionLine())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// defaultHome возвращает дом данных agctl: $AGCTL_HOME, если задан,
// иначе ~/.agctl (или .agctl в текущем каталоге, если домашний каталог
// определить не удалось).
func defaultHome() string {
	if h := os.Getenv("AGCTL_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agctl"
	}
	return filepath.Join(home, ".agctl")
}

// requireHome создаёт каталог дома при необходимости и возвращает его
// абсолютный путь; ошибка создания оборачивается контекстом имени дома.
func requireHome(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("home %s: %w", dir, err)
	}
	return filepath.Abs(dir)
}

// regLimitValue — флаг 0 = явное отключение защиты; в Config это
// отрицательное значение (ноль в Config — платформенный дефолт 30/мин).
func regLimitValue(v int) int {
	if v == 0 {
		return -1
	}
	return v
}

// cmdServe — подкоманда serve: разрешает профиль организации, собирает
// конфигурацию из флагов/среды/профиля, открывает дом (мастер-ключ + SQLite),
// инициализирует LLM-провайдера по умолчанию, строит server.Server и слушает
// адрес до сигнала завершения (graceful shutdown с остановкой рантаймов).
func cmdServe(args []string) { cmdServeWith(args, nil, nil) }

// cmdServeWith — serve с опциональным локальным режимом (agctl dev serve):
// local != nil включает однопользовательский режим, onReady вызывается с
// готовым обработчиком до ListenAndServe (доп. слушатели, файл состояния).
func cmdServeWith(args []string, local *server.LocalMode, onReady func(context.Context, http.Handler)) {
	// Профиль разрешается до регистрации флагов: от него зависят дефолты
	// (LLM-провайдер и брендинг сервера). Имя ищется во встроенных профилях,
	// Дефолты: флаги > env > ~/.config/agctl/config.yaml > встроенные.
	// Конфиг читаем до разбора флагов: его значения — дефолты serve.
	cfg, cfgPath, cfgErr := loadUserConfig()
	if cfgErr != nil {
		log.Fatal(cfgErr)
	}
	if cfgPath != "" {
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			log.Printf("config: %s", cfgPath)
		}
	}
	// затем в ./profiles и $AGCTL_HOME/profiles (make profile-fetch).
	profileName := prescanFlag(args, "profile", envOr("AGCTL_PROFILE", firstNonEmpty(cfg.Profile, "agctl")))
	profileDir := prescanFlag(args, "profile-dir", firstNonEmpty(os.Getenv("AGCTL_PROFILE_DIR"), cfg.ProfileDir))
	prof, err := profile.Resolve(profileName, profileDir, "profiles", filepath.Join(defaultHome(), "profiles"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("profile: %s (%s)", prof.ID, prof.Source)

	fsSet := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	addr := fsSet.String("addr", envOr("AGCTL_ADDR", firstNonEmpty(cfg.Serve.Addr, "127.0.0.1:8929")), "адрес UI/API")
	image := fsSet.String("image", firstNonEmpty(cfg.Serve.Image, "agctl-dsh:0.2.2"), "тег образа agctl-dsh")
	devImages := fsSet.String("dev-images", envOr("AGCTL_DEV_IMAGES", firstNonEmpty(cfg.Serve.DevImages, strings.Join(prof.Images.Dev, ","))), "каталог образов лаунчера developer'а через запятую (реестр Nexus и пр.; по умолчанию — images.dev профиля); пусто = только --image")
	devEnv := fsSet.String("dev-env", envOr("AGCTL_DEV_ENV", cfg.Serve.DevEnv), "имена переменных окружения хоста, пробрасываемые в контейнер developer'а через запятую (значения из .env); пусто = не пробрасывать")
	binDir := fsSet.String("bin-dir", "", "каталог linux-сборок agctl для образа (по умолчанию ./dist)")
	secureCookies := fsSet.Bool("secure-cookies", false, "выставлять Secure на cookie (если TLS завершается на доверенном ingress/reverse proxy)")
	publicURL := fsSet.String("public-url", envOr("AGCTL_PUBLIC_URL", cfg.Serve.PublicURL), "базовый URL шлюза, видимый песочницам (fetch-at-boot раннера AX); пусто = http://<первый не-loopback IP>:<порт --addr>")
	experimentalLocalBridge := fsSet.Bool("experimental-local-bridge", false, "experimental local connector pairing API (requires trusted TLS ingress and --secure-cookies)")
	chatTTLMin := fsSet.Int("chat-ttl-min", nonZeroOr(cfg.Serve.ChatTTLMin, 60), "минуты простоя персонального контейнера до сна (том пользователя сохраняется)")
	registerPerMin := fsSet.Int("register-per-min", nonZeroOr(cfg.Serve.RegisterPerMin, 30), "лимит регистраций с одного IP в минуту; 0 — отключить защиту (нагрузочные прогоны)")
	corsOrigins := fsSet.String("cors-origin", envOr("AGCTL_CORS_ORIGINS", cfg.Serve.CORSOrigins), "точные origins через запятую, которым разрешён кросс-доменный /api (кабинет-плагин dsh web); пусто = CORS выключен")
	tier0 := fsSet.Bool("tier0", true, "поднимать общий чат (Tier 0) и отдавать корень платформы в него")
	memGB := fsSet.Float64("mem-gb", nonZeroOr(cfg.Serve.MemGB, 2), "лимит памяти контейнера, ГБ")
	cpus := fsSet.Float64("cpus", nonZeroOr(cfg.Serve.CPUs, 2), "лимит CPU контейнера")
	profileFlag := fsSet.String("profile", profileName, "профиль платформы: встроенный agctl (нейтральный) или имя каталога из ./profiles и $AGCTL_HOME/profiles")
	profileDirFlag := fsSet.String("profile-dir", profileDir, "каталог профиля с диска (profile.yaml + assets) — кастомизация без пересборки бинаря")
	// Платформенный LLM по умолчанию для режима Chat: дефолты приходят из
	// профиля, env их перекрывает; ключ хранится только в env.
	llmBaseURL := fsSet.String("llm-base-url", envOr("AGCTL_LLM_BASE_URL", prof.LLM.DefaultBaseURL), "базовый URL OpenAI-совместимого провайдера чата по умолчанию (пусто = платформа без провайдера по умолчанию)")
	llmModel := fsSet.String("llm-model", envOr("AGCTL_LLM_MODEL", prof.LLM.DefaultModel), "модель чата по умолчанию (из списка llm-models)")
	llmModels := fsSet.String("llm-models", envOr("AGCTL_LLM_MODELS", prof.LLM.DefaultModels), "список моделей чата через запятую")
	llmAPIKey := fsSet.String("llm-api-key", strings.TrimSpace(envOr("AGCTL_LLM_API_KEY", "")), "ключ провайдера чата (предпочтительно через env AGCTL_LLM_API_KEY)")
	rf := registerRuntimeFlags(fsSet, cfg)
	_ = profileFlag
	_ = profileDirFlag
	_ = fsSet.Parse(args)
	baseImage, workImage := profileImages(fsSet, prof, *image)
	if workImage != baseImage {
		log.Printf("образ: %s (надстройка профиля %s поверх %s)", workImage, prof.ID, baseImage)
	}
	*llmAPIKey = strings.TrimSpace(*llmAPIKey)
	for _, r := range *llmAPIKey {
		if r < 0x20 || r == 0x7f {
			log.Fatal("AGCTL_LLM_API_KEY должен содержать только raw-токен в одну строку (без Bearer, кавычек и переводов строки)")
		}
	}
	if *experimentalLocalBridge && !*secureCookies && !isLoopbackAddr(*addr) {
		log.Fatal("--experimental-local-bridge requires --secure-cookies behind a trusted TLS ingress (plain loopback --addr is allowed for local development only)")
	}

	defaultLLM := provision.Provider{
		Name:    llmProviderName(prof),
		BaseURL: *llmBaseURL,
		Model:   *llmModel,
		Models:  splitCSV(*llmModels),
		APIKey:  *llmAPIKey,
		Extras:  profExtraProviders(prof),
	}
	if defaultLLM.Model == "" && len(defaultLLM.Models) > 0 {
		defaultLLM.Model = defaultLLM.Models[0]
	}

	absHome, err := requireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	keyPath, err := secrets.EnsureMasterKey(absHome)
	if err != nil {
		log.Fatal(err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	srvCfg := server.Config{
		Home:                    absHome,
		Addr:                    *addr,
		MemoryBytes:             int64(*memGB * 1024 * 1024 * 1024),
		NanoCPUs:                int64(*cpus * 1e9),
		Image:                   workImage,
		BaseImage:               baseImage,
		DevImages:               splitCSV(*devImages),
		DevEnv:                  splitCSV(*devEnv),
		BinDir:                  firstNonEmpty(*binDir, "dist"),
		Version:                 version,
		SecureCookies:           *secureCookies,
		PublicURL:               derivePublicURL(*publicURL, *addr),
		ExperimentalLocalBridge: *experimentalLocalBridge,
		CORSOrigins:             splitCSV(*corsOrigins),
		Tier0:                   *tier0,
		ChatTTL:                 time.Duration(*chatTTLMin) * time.Minute,
		RegisterPerMinute:       regLimitValue(*registerPerMin),
		DefaultLLM:              defaultLLM,
		Profile:                 prof,
		Local:                   local,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if local != nil {
		local.Shutdown = stop
	}
	srv, err := buildServer(rf, db, sec, srvCfg)
	if defaultLLM.BaseURL != "" {
		log.Printf("llm: платформенный провайдер чата по умолчанию %s (%s, моделей: %d, доп. провайдеров: %d, ключ %s)", defaultLLM.BaseURL, defaultLLM.Alias(), len(defaultLLM.AllModels()), len(defaultLLM.Extras), keyState(defaultLLM.APIKey))
	} else {
		log.Printf("llm: провайдер по умолчанию не задан (профиль %s без llm-дефолтов; задайте env AGCTL_LLM_* или профиль организации)", prof.ID)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()

	webFS, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	srv.StartBackground(ctx)

	handler := srv.Routes(webFS)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if onReady != nil {
		onReady(ctx, handler)
	}
	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()
	log.Printf("agctl serve: home=%s addr=http://%s (UI и API)", absHome, *addr)
	runtimeKind, _ := parseRuntimeKind(*rf.kind)
	log.Printf("%s", runtimeStatusLine(runtimeKind))
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}

// llmProviderName — алиас основного провайдера в dsh: профиль задаёт
// provider_name (например zai-coding-cn), env AGCTL_LLM_PROVIDER перекрывает.
func llmProviderName(prof *profile.Data) string {
	if name := strings.TrimSpace(os.Getenv("AGCTL_LLM_PROVIDER")); name != "" {
		return name
	}
	if prof != nil {
		return strings.TrimSpace(prof.LLM.ProviderName)
	}
	return ""
}

// profExtraProviders — дополнительные провайдеры из профиля (или из
// AGCTL_LLM_EXTRA_PROVIDERS для оркестрированных запусков). В профиле
// хранится только имя env-переменной; значение ключа читаем из окружения и
// никогда не пишем в settings.yaml/профиль/лог.
func profExtraProviders(prof *profile.Data) []provision.ExtraProvider {
	var extras []provision.ExtraProvider
	if raw := strings.TrimSpace(os.Getenv("AGCTL_LLM_EXTRA_PROVIDERS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &extras); err != nil {
			log.Printf("llm: AGCTL_LLM_EXTRA_PROVIDERS: %v — использую профиль", err)
			extras = nil
		}
	}
	if extras == nil && prof != nil {
		for _, e := range prof.LLM.ExtraProviders {
			name := strings.TrimSpace(e.Name)
			baseURL := strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
			if name == "" || baseURL == "" {
				continue
			}
			models := provision.ParseModels(e.Models, e.Model)
			model := strings.TrimSpace(e.Model)
			if model == "" && len(models) > 0 {
				model = models[0]
			}
			extras = append(extras, provision.ExtraProvider{
				Name: name, BaseURL: baseURL, Model: model, Models: models,
				APIKeyEnv: strings.TrimSpace(e.APIKeyEnv),
			})
		}
	}
	for i := range extras {
		env := strings.TrimSpace(extras[i].APIKeyEnv)
		if env == "" {
			env = extras[i].KeyEnv()
		}
		extras[i].APIKeyEnv = env
		extras[i].APIKey = strings.TrimSpace(os.Getenv(env))
	}
	return extras
}

// cmdMigrate — подкоманда migrate: открывает базу и применяет все
// неприменённые миграции схемы до актуального состояния.
func cmdMigrate(args []string) {
	fsSet := flag.NewFlagSet("migrate", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	_ = fsSet.Parse(args)
	absHome, err := requireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("migrate: схема актуальна:", filepath.Join(absHome, "agctl.db"))
}

// cmdInitChatHome seeds/refreshes the DSH_HOME PVC from a Kubernetes init
// container. Provider data arrives only through env/Secret, never CLI args.
func cmdInitChatHome(args []string) {
	fsSet := flag.NewFlagSet("init-chat-home", flag.ExitOnError)
	home := fsSet.String("home", os.Getenv("DSH_HOME"), "каталог DSH_HOME на PVC")
	refresh := fsSet.Bool("refresh-provider", os.Getenv("AGCTL_REFRESH_PROVIDER") == "1", "обновить настройки провайдера")
	_ = fsSet.Parse(args)
	if *home == "" {
		log.Fatal("init-chat-home: требуется --home или DSH_HOME")
	}
	// Пресет разрешений роли user (danger-full-access) — как у Docker-адаптера.
	fullAccess := os.Getenv("AGCTL_DEFAULT_FULL_ACCESS") == "1"
	if err := prepareChatHome(*home, providerFromEnv(), *refresh, fullAccess); err != nil {
		log.Fatal(err)
	}
}

// initChatHome готовит DSH_HOME для init-контейнера Kubernetes: сеет скелет
// в пустой том, при refresh обновляет настройки LLM-провайдера, иначе
// оставляет существующий дом нетронутым. Возвращает флаги, что именно сделано.
func initChatHome(home string, provider provision.Provider, refresh bool, skel fs.FS) (seeded, refreshed bool, err error) {
	if home == "" {
		return false, false, fmt.Errorf("init-chat-home: home is empty")
	}
	profile := filepath.Join(home, "profiles", "web", "package.json")
	if _, statErr := os.Stat(profile); os.IsNotExist(statErr) {
		if err := provision.SeedDshHome(home, provider, skel); err != nil {
			return false, false, err
		}
		return true, false, nil
	} else if statErr != nil {
		return false, false, statErr
	}
	if refresh {
		if err := provision.RefreshProvider(home, provider); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	return false, false, nil
}

// dockerStatus возвращает человекочитаемую строку о доступности Docker:
// найден ли ожидаемый путь сокета/пайпа демона на текущей ОС.
func dockerStatus() string {
	if _, err := os.Stat(dockerHintPath()); err == nil {
		return "сокет найден"
	}
	return "если демон не запущен — стартуйте Docker Desktop / dockerd"
}

// dockerHintPath возвращает ожидаемый путь к сокету Docker для текущей
// платформы (macOS: ~/.docker/run/docker.sock, Windows: named pipe,
// прочие: /var/run/docker.sock) — только для подсказки в логе.
func dockerHintPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".docker", "run", "docker.sock")
	case "windows":
		return `\\.\pipe\docker_engine`
	default:
		return "/var/run/docker.sock"
	}
}

// envOr возвращает значение переменной среды key или def, если она пуста
// либо не задана.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// firstNonEmpty возвращает первую непустую строку из аргументов или пустую
// строку, если все пусты.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// nonZeroOr — численный аналог firstNonEmpty для дефолтов из config.yaml:
// нулевое значение конфига означает «не задано» (0 в yaml — очевидный маркер
// отсутствия настройки, отдельный «нулевой» лимит смысла не имеет).
func nonZeroOr[T int | float64](v, def T) T {
	if v != 0 {
		return v
	}
	return def
}

// splitCSV разбивает строку по запятым, обрезая пробелы и пропуская пустые
// элементы.
func splitCSV(s string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// keyState — состояние секрета для логов: только факт наличия, не значение.
func keyState(secret string) string {
	if secret == "" {
		return "не задан"
	}
	return "задан"
}

// isLoopbackAddr — true для адресов вида 127.0.0.1:p, [::1]:p, localhost:p.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// profileImages — образ рабочего места с учётом надстройки профиля
// (images.dockerfile): без явного --image рабочий образ —
// agctl-dsh-<профиль>:<тег> поверх базового. Явный --image — как есть.
func profileImages(fsSet *flag.FlagSet, prof *profile.Data, image string) (base, work string) {
	explicit := false
	fsSet.Visit(func(f *flag.Flag) {
		if f.Name == "image" {
			explicit = true
		}
	})
	if explicit || prof == nil || prof.Images.Dockerfile == "" {
		return image, image
	}
	return image, profile.OverlayImage(image, prof.ID)
}

// derivePublicURL — базовый URL шлюза для песочниц: явный --public-url,
// иначе http://<не-loopback IPv4 хоста>:<порт --addr>; не нашлось — адрес
// как задан (без схемы).
func derivePublicURL(publicURL, addr string) string {
	if publicURL != "" {
		return strings.TrimRight(publicURL, "/")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = outboundIPv4()
	}
	if host == "" {
		return "http://" + addr
	}
	return "http://" + net.JoinHostPort(host, port)
}

// outboundIPv4 — адрес интерфейса с маршрутом по умолчанию (как docker
// bridge видит хост); пусто — не нашлось.
func outboundIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return ""
	}
	return host
}
