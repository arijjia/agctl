package cli

// serve.go — общий `serve` всех точек входа: agctl-server serve (сервер
// организации) и agctl dev serve (локальный режим). Различия задают
// ServeOptions: локальный режим, модули сервера и бэкенд рантаймов.

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
	"github.com/arijjia/agctl/web"
)

// regLimitValue — флаг 0 = явное отключение защиты; в Config это
// отрицательное значение (ноль в Config — платформенный дефолт 30/мин).
func regLimitValue(v int) int {
	if v == 0 {
		return -1
	}
	return v
}

// Runtime — бэкенд персональных рантаймов, выбранный флагами serve.
type Runtime interface {
	// Build строит сервер поверх бэкенда.
	Build(db *store.DB, sec *secrets.Manager, cfg server.Config) (*server.Server, error)
	// StatusLine — строка лога старта о бэкенде.
	StatusLine() string
}

// RuntimeFlags регистрирует флаги бэкенда в flagset serve и возвращает
// бэкенд, который читает их после разбора.
type RuntimeFlags func(fs *flag.FlagSet, cfg UserConfig) Runtime

// ServeOptions — чем точки входа отличаются в serve.
type ServeOptions struct {
	// Local включает однопользовательский локальный режим (agctl dev serve).
	Local *server.LocalMode
	// OnReady вызывается с готовым обработчиком до ListenAndServe (доп.
	// слушатели, файл состояния).
	OnReady func(context.Context, http.Handler)
	// Modules — серверные модули (server.ServerModules() у agctl-server).
	Modules []server.Module
	// Runtime — флаги и сборка бэкенда; nil = Docker на этой машине.
	Runtime RuntimeFlags
	// Profile — уже разрешённый профиль (локальный режим: его разрешает
	// localmode.ResolveProfile — один порядок для CLI и serve). nil = serve
	// разрешает сам по --profile/--profile-dir (сервер организации).
	Profile *profile.Data
}

// DockerRuntime — бэкенд по умолчанию: контейнеры на локальном Docker.
func DockerRuntime(*flag.FlagSet, UserConfig) Runtime { return dockerRuntime{} }

type dockerRuntime struct{}

func (dockerRuntime) Build(db *store.DB, sec *secrets.Manager, cfg server.Config) (*server.Server, error) {
	return server.New(db, sec, cfg)
}

func (dockerRuntime) StatusLine() string { return "docker: " + DockerStatus() }

// Serve — подкоманда serve: разрешает профиль организации, собирает
// конфигурацию из флагов/среды/профиля, открывает дом (мастер-ключ + SQLite),
// строит server.Server на выбранном бэкенде и слушает адрес до сигнала
// завершения (graceful shutdown с остановкой рантаймов).
func Serve(args []string, opts ServeOptions) {
	local, onReady := opts.Local, opts.OnReady
	runtimeFlags := opts.Runtime
	if runtimeFlags == nil {
		runtimeFlags = DockerRuntime
	}
	// Профиль разрешается до регистрации флагов: от него зависят дефолты
	// (LLM-провайдер и брендинг сервера). Имя ищется во встроенных профилях,
	// Дефолты: флаги > env > ~/.config/agctl/config.yaml > встроенные.
	// Конфиг читаем до разбора флагов: его значения — дефолты serve.
	cfg, cfgPath, cfgErr := LoadUserConfig()
	if cfgErr != nil {
		log.Fatal(cfgErr)
	}
	if cfgPath != "" {
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			log.Printf("config: %s", cfgPath)
		}
	}
	// затем в ./profiles и $AGCTL_HOME/profiles (make profile-fetch).
	profileName := PrescanFlag(args, "profile", EnvOr("AGCTL_PROFILE", FirstNonEmpty(cfg.Profile, "agctl")))
	profileDir := PrescanFlag(args, "profile-dir", FirstNonEmpty(os.Getenv("AGCTL_PROFILE_DIR"), cfg.ProfileDir))
	prof := opts.Profile
	if prof == nil {
		var err error
		prof, err = profile.Resolve(profileName, profileDir, "profiles", filepath.Join(DefaultHome(), "profiles"))
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("profile: %s (%s)", prof.ID, prof.Source)

	fsSet := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fsSet.String("home", DefaultHome(), "каталог данных agctl")
	addr := fsSet.String("addr", EnvOr("AGCTL_ADDR", FirstNonEmpty(cfg.Serve.Addr, "127.0.0.1:8929")), "адрес UI/API")
	image := fsSet.String("image", FirstNonEmpty(cfg.Serve.Image, "agctl-dsh-server:0.2.2"), "тег серверного образа рабочего места agctl-dsh-server (guest: ax-runner, init-chat-home; надстройка профиля — agctl-dsh-server-<профиль>)")
	devImages := fsSet.String("dev-images", EnvOr("AGCTL_DEV_IMAGES", FirstNonEmpty(cfg.Serve.DevImages, strings.Join(prof.Images.Dev, ","))), "каталог образов лаунчера developer'а через запятую (реестр Nexus и пр.; по умолчанию — images.dev профиля); пусто = только --image")
	devEnv := fsSet.String("dev-env", EnvOr("AGCTL_DEV_ENV", cfg.Serve.DevEnv), "имена переменных окружения хоста, пробрасываемые в контейнер developer'а через запятую (значения из .env); пусто = не пробрасывать")
	binDir := fsSet.String("bin-dir", "", "каталог linux-сборок agctl для образа (по умолчанию ./dist)")
	secureCookies := fsSet.Bool("secure-cookies", false, "выставлять Secure на cookie (если TLS завершается на доверенном ingress/reverse proxy)")
	publicURL := fsSet.String("public-url", EnvOr("AGCTL_PUBLIC_URL", cfg.Serve.PublicURL), "базовый URL шлюза, видимый песочницам (fetch-at-boot раннера AX); пусто = http://<первый не-loopback IP>:<порт --addr>")
	chatTTLMin := fsSet.Int("chat-ttl-min", NonZeroOr(cfg.Serve.ChatTTLMin, 60), "минуты простоя персонального контейнера до сна (том пользователя сохраняется)")
	registerPerMin := fsSet.Int("register-per-min", NonZeroOr(cfg.Serve.RegisterPerMin, 30), "лимит регистраций с одного IP в минуту; 0 — отключить защиту (нагрузочные прогоны)")
	corsOrigins := fsSet.String("cors-origin", EnvOr("AGCTL_CORS_ORIGINS", cfg.Serve.CORSOrigins), "точные origins через запятую, которым разрешён кросс-доменный /api (кабинет-плагин dsh web); пусто = CORS выключен")
	egressAllowlist := fsSet.Bool("egress-allowlist", EnvOr("AGCTL_EGRESS_ALLOWLIST", boolStr(cfg.Serve.EgressAllowlist)) == "1", "Docker-бэкенд: контейнеры рабочих мест на выделенной сети, наружу только DNS, хост (host.docker.internal), провайдеры модели и белый список (--egress-allow, профиль egress.allow); остальное отбрасывается")
	egressAllow := fsSet.String("egress-allow", EnvOr("AGCTL_EGRESS_ALLOW", cfg.Serve.EgressAllow), "белый список egress сверх профиля: хосты, IPv4 или CIDR через запятую (GitLab, Jira, реестр); действует с --egress-allowlist")
	tier0 := fsSet.Bool("tier0", true, "поднимать общий чат (Tier 0) и отдавать корень платформы в него")
	memGB := fsSet.Float64("mem-gb", NonZeroOr(cfg.Serve.MemGB, 2), "лимит памяти контейнера, ГБ")
	cpus := fsSet.Float64("cpus", NonZeroOr(cfg.Serve.CPUs, 2), "лимит CPU контейнера")
	profileFlag := fsSet.String("profile", profileName, "профиль платформы: встроенный agctl (нейтральный) или имя каталога из ./profiles и $AGCTL_HOME/profiles")
	profileDirFlag := fsSet.String("profile-dir", profileDir, "каталог профиля с диска (profile.yaml + assets) — кастомизация без пересборки бинаря")
	// Платформенный LLM по умолчанию для режима Chat: дефолты приходят из
	// профиля, env их перекрывает; ключ хранится только в env.
	llmBaseURL := fsSet.String("llm-base-url", EnvOr("AGCTL_LLM_BASE_URL", prof.LLM.DefaultBaseURL), "базовый URL OpenAI-совместимого провайдера чата по умолчанию (пусто = платформа без провайдера по умолчанию)")
	llmModel := fsSet.String("llm-model", EnvOr("AGCTL_LLM_MODEL", prof.LLM.DefaultModel), "модель чата по умолчанию (из списка llm-models)")
	llmModels := fsSet.String("llm-models", EnvOr("AGCTL_LLM_MODELS", prof.LLM.DefaultModels), "список моделей чата через запятую")
	llmAPIKey := fsSet.String("llm-api-key", strings.TrimSpace(EnvOr("AGCTL_LLM_API_KEY", "")), "ключ провайдера чата (предпочтительно через env AGCTL_LLM_API_KEY)")
	rt := runtimeFlags(fsSet, cfg)
	_ = profileFlag
	_ = profileDirFlag
	_ = fsSet.Parse(args)
	baseImage, workImage := ProfileImages(fsSet, prof, *image)
	if workImage != baseImage {
		log.Printf("образ: %s (надстройка профиля %s поверх %s)", workImage, prof.ID, baseImage)
	}
	*llmAPIKey = strings.TrimSpace(*llmAPIKey)
	for _, r := range *llmAPIKey {
		if r < 0x20 || r == 0x7f {
			log.Fatal("AGCTL_LLM_API_KEY должен содержать только raw-токен в одну строку (без Bearer, кавычек и переводов строки)")
		}
	}

	defaultLLM := provision.Provider{
		Name:    LLMProviderName(prof),
		BaseURL: *llmBaseURL,
		Model:   *llmModel,
		Models:  SplitCSV(*llmModels),
		APIKey:  *llmAPIKey,
		Extras:  ProfExtraProviders(prof),
	}
	if defaultLLM.Model == "" && len(defaultLLM.Models) > 0 {
		defaultLLM.Model = defaultLLM.Models[0]
	}

	absHome, err := RequireHome(*home)
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
		Home:              absHome,
		Addr:              *addr,
		MemoryBytes:       int64(*memGB * 1024 * 1024 * 1024),
		NanoCPUs:          int64(*cpus * 1e9),
		Image:             workImage,
		BaseImage:         baseImage,
		DevImages:         SplitCSV(*devImages),
		DevEnv:            SplitCSV(*devEnv),
		BinDir:            FirstNonEmpty(*binDir, "dist"),
		Version:           version,
		Modules:           opts.Modules,
		SecureCookies:     *secureCookies,
		PublicURL:         DerivePublicURL(*publicURL, *addr),
		CORSOrigins:       SplitCSV(*corsOrigins),
		Tier0:             *tier0,
		EgressAllowlist:   *egressAllowlist,
		EgressAllow:       append(append([]string(nil), prof.Egress.Allow...), SplitCSV(*egressAllow)...),
		ChatTTL:           time.Duration(*chatTTLMin) * time.Minute,
		RegisterPerMinute: regLimitValue(*registerPerMin),
		DefaultLLM:        defaultLLM,
		Profile:           prof,
		Local:             local,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if local != nil {
		local.Shutdown = stop
	}
	srv, err := rt.Build(db, sec, srvCfg)
	if defaultLLM.BaseURL != "" {
		log.Printf("llm: платформенный провайдер чата по умолчанию %s (%s, моделей: %d, доп. провайдеров: %d, ключ %s)", defaultLLM.BaseURL, defaultLLM.Alias(), len(defaultLLM.AllModels()), len(defaultLLM.Extras), KeyState(defaultLLM.APIKey))
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
	log.Printf("%s serve: home=%s addr=http://%s (UI и API)", binName, absHome, *addr)
	log.Printf("%s", rt.StatusLine())
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}

// LLMProviderName — алиас основного провайдера в dsh: профиль задаёт
// provider_name (например zai-coding-cn), env AGCTL_LLM_PROVIDER перекрывает.
func LLMProviderName(prof *profile.Data) string {
	if name := strings.TrimSpace(os.Getenv("AGCTL_LLM_PROVIDER")); name != "" {
		return name
	}
	if prof != nil {
		return strings.TrimSpace(prof.LLM.ProviderName)
	}
	return ""
}

// ProfExtraProviders — дополнительные провайдеры из профиля (или из
// AGCTL_LLM_EXTRA_PROVIDERS для оркестрированных запусков). В профиле
// хранится только имя env-переменной; значение ключа читаем из окружения и
// никогда не пишем в settings.yaml/профиль/лог.
func ProfExtraProviders(prof *profile.Data) []provision.ExtraProvider {
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

// ProfileImages — образ рабочего места с учётом надстройки профиля
// (images.dockerfile): без явного --image рабочий образ —
// agctl-dsh-<профиль>:<тег> поверх базового. Явный --image — как есть.
func ProfileImages(fsSet *flag.FlagSet, prof *profile.Data, image string) (base, work string) {
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

// DerivePublicURL — базовый URL шлюза для песочниц: явный --public-url,
// иначе http://<не-loopback IPv4 хоста>:<порт --addr>; не нашлось — адрес
// как задан (без схемы).
func DerivePublicURL(publicURL, addr string) string {
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
