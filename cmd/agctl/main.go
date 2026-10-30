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

	"github.com/arijjia/agctl/internal/assets"
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

// Дефолтов LLM в коде больше нет: платформа универсальна, а организация
// задаётся профилем (internal/profile). Нейтральный agctl — без LLM-дефолтов;
// профиль организации (например utmn) несёт свои.

// prescanFlag достаёт значение флага из argv до штатного разбора (нужен,
// чтобы дефолты остальных флагов зависели от профиля). Поддерживает
// --name=v и --name v.
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
	case "seed-demo-users":
		cmdSeedDemoUsers(os.Args[2:])
	case "backlog":
		os.Exit(backlog.Main(os.Args[2:], os.Stdout, os.Stderr))
	case "workspace":
		cmdWorkspace(os.Args[2:])
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
	case "version":
		fmt.Printf("agctl %s (%s/%s %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `agctl — платформа агентских рабочих мест (один бинарь)

Команды:
  serve [--home DIR] [--addr H:P] [--image T] [--bin-dir D]
  migrate [--home DIR]
  seed-demo-users [--home DIR]  (локальные тестовые роли; запускать после первого admin)
  backlog <new|list|top|check|start|done|cancel|note|migrate> [флаги]
  workspace (legacy create отключён в v3)
  connect --port SSH_PORT [--host HOST] [--user root] [--identity KEY] [--print]
  local connect --server URL --grant TOKEN [--local 127.0.0.1:3080] [--launch-dsh]
      [--dsh-bin dsh] [--dsh-port 3080] [--dsh-token TOKEN]
  sync create --name NAME --local DIR --host HOST --port SSH_PORT [--user root] [--print]
  sync list|pause|resume|flush|terminate [Mutagen args]
  version

Переменные окружения: AGCTL_HOME (по умолчанию ~/.agctl), AGCTL_ADDR.
`)
}

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

func requireHome(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("home %s: %w", dir, err)
	}
	return filepath.Abs(dir)
}

func cmdServe(args []string) {
	// Профиль разрешается до регистрации флагов: от него зависят дефолты
	// (LLM-провайдер и брендинг сервера).
	profileName := prescanFlag(args, "profile", envOr("AGCTL_PROFILE", "agctl"))
	profileDir := prescanFlag(args, "profile-dir", os.Getenv("AGCTL_PROFILE_DIR"))
	prof, err := profile.Resolve(profileName, profileDir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("profile: %s (%s)", prof.ID, prof.Source)

	fsSet := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	addr := fsSet.String("addr", envOr("AGCTL_ADDR", "127.0.0.1:8929"), "адрес UI/API")
	image := fsSet.String("image", "agctl-dsh:0.2.2", "тег образа agctl-dsh")
	binDir := fsSet.String("bin-dir", "", "каталог linux-сборок agctl для образа (по умолчанию ./dist)")
	secureCookies := fsSet.Bool("secure-cookies", false, "выставлять Secure на cookie (если TLS завершается на доверенном ingress/reverse proxy)")
	experimentalLocalBridge := fsSet.Bool("experimental-local-bridge", false, "experimental local connector pairing API (requires trusted TLS ingress and --secure-cookies)")
	chatTTLMin := fsSet.Int("chat-ttl-min", 60, "минуты простоя персонального контейнера до сна (том пользователя сохраняется)")
	corsOrigins := fsSet.String("cors-origin", envOr("AGCTL_CORS_ORIGINS", ""), "точные origins через запятую, которым разрешён кросс-доменный /api (кабинет-плагин dsh web); пусто = CORS выключен")
	tier0 := fsSet.Bool("tier0", true, "поднимать общий чат (Tier 0) и отдавать корень платформы в него")
	memGB := fsSet.Float64("mem-gb", 2, "лимит памяти контейнера, ГБ")
	cpus := fsSet.Float64("cpus", 2, "лимит CPU контейнера")
	profileFlag := fsSet.String("profile", profileName, "встроенный профиль платформы (по умолчанию agctl; профиль организации utmn встроен до выделения в отдельный репо)")
	profileDirFlag := fsSet.String("profile-dir", profileDir, "каталог профиля с диска (profile.yaml + assets) — кастомизация без пересборки бинаря")
	// Платформенный LLM по умолчанию для режима Chat: дефолты приходят из
	// профиля, env их перекрывает; ключ хранится только в env.
	llmBaseURL := fsSet.String("llm-base-url", envOr("AGCTL_LLM_BASE_URL", prof.LLM.DefaultBaseURL), "базовый URL OpenAI-совместимого провайдера чата по умолчанию (пусто = платформа без провайдера по умолчанию)")
	llmModel := fsSet.String("llm-model", envOr("AGCTL_LLM_MODEL", prof.LLM.DefaultModel), "модель чата по умолчанию (из списка llm-models)")
	llmModels := fsSet.String("llm-models", envOr("AGCTL_LLM_MODELS", prof.LLM.DefaultModels), "список моделей чата через запятую")
	llmAPIKey := fsSet.String("llm-api-key", strings.TrimSpace(envOr("AGCTL_LLM_API_KEY", "")), "ключ провайдера чата (предпочтительно через env AGCTL_LLM_API_KEY)")
	_ = profileFlag
	_ = profileDirFlag
	_ = fsSet.Parse(args)
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
		BaseURL: *llmBaseURL,
		Model:   *llmModel,
		Models:  splitCSV(*llmModels),
		APIKey:  *llmAPIKey,
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
	srv, err := server.New(db, sec, server.Config{
		Home:                    absHome,
		Addr:                    *addr,
		MemoryBytes:             int64(*memGB * 1024 * 1024 * 1024),
		NanoCPUs:                int64(*cpus * 1e9),
		Image:                   *image,
		BinDir:                  firstNonEmpty(*binDir, "dist"),
		SecureCookies:           *secureCookies,
		ExperimentalLocalBridge: *experimentalLocalBridge,
		CORSOrigins:             splitCSV(*corsOrigins),
		Tier0:                   *tier0,
		ChatTTL:                 time.Duration(*chatTTLMin) * time.Minute,
		DefaultLLM:              defaultLLM,
		Profile:                 prof,
	})
	if defaultLLM.BaseURL != "" {
		log.Printf("llm: платформенный провайдер чата по умолчанию %s (моделей: %d, ключ %s)", defaultLLM.BaseURL, len(defaultLLM.AllModels()), keyState(defaultLLM.APIKey))
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.StartBackground(ctx)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(webFS),
		ReadHeaderTimeout: 10 * time.Second,
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
	log.Printf("docker: %s", dockerStatus())
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}

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
	models := []string{}
	if raw := os.Getenv("AGCTL_LLM_MODELS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			log.Fatalf("init-chat-home: AGCTL_LLM_MODELS: %v", err)
		}
	}
	provider := provision.Provider{
		BaseURL: os.Getenv("AGCTL_LLM_BASE_URL"),
		Model:   os.Getenv("AGCTL_LLM_MODEL"),
		Models:  models,
		APIKey:  os.Getenv("AGCTL_LLM_API_KEY"),
	}
	seeded, refreshed, err := initChatHome(*home, provider, *refresh, assets.DshSkel)
	if err != nil {
		log.Fatal(err)
	}
	if err := provision.EnsureChatPlugins(*home, assets.DshSkel); err != nil {
		log.Fatal(err)
	}
	switch {
	case seeded:
		fmt.Println("init-chat-home: seeded")
	case refreshed:
		fmt.Println("init-chat-home: provider refreshed")
	default:
		fmt.Println("init-chat-home: existing home preserved")
	}
}

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

func dockerStatus() string {
	if _, err := os.Stat(dockerHintPath()); err == nil {
		return "сокет найден"
	}
	return "если демон не запущен — стартуйте Docker Desktop / dockerd"
}

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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

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
