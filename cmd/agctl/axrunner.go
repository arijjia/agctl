package main

// axrunner.go — `agctl ax-runner`: PID 1 песочницы Google AX / Agent Substrate
// (контракт раннера AX, docs/runner.md апстрима): читает AX_TASK_YAML, готовит
// DSH_HOME на долговечном томе /workspace, запускает agctl-entrypoint (dsh web)
// и держит HTTP :80 — /healthz, /readyz (dsh отвечает), реверс-прокси на dsh
// web :3080 (маршрутизатор Substrate ходит в актор по :80) и /agctl/boot-log —
// хвост лога запуска dsh (boot-токен) для шлюза agctl, по токену раннера.
// В образе agctl-dsh на него указывает обёртка /usr/local/bin/ax-task-runner.

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/axrt"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

// runnerTask — снисходительный разбор Task AX (только нужные поля; новые
// поля апстрима не ломают раннер).
type runnerTask struct {
	Metadata struct {
		Name     string `yaml:"name"`
		Atespace string `yaml:"atespace"`
	} `yaml:"metadata"`
	Spec struct {
		Command []string `yaml:"command"`
		Debug   bool     `yaml:"debug"`
		Env     []struct {
			Name  string `yaml:"name"`
			Value string `yaml:"value"`
		} `yaml:"env"`
	} `yaml:"spec"`
}

// defaultRunnerCommand — команда песочницы по умолчанию: entrypoint образа
// agctl-dsh (сеет скелет, поднимает dsh web на eth0:3080).
var defaultRunnerCommand = []string{"/usr/local/bin/agctl-entrypoint"}

// parseRunnerTask — Task из YAML (AX_TASK_YAML); пустой ввод = задача по
// умолчанию.
func parseRunnerTask(raw string) (runnerTask, error) {
	var task runnerTask
	if strings.TrimSpace(raw) == "" {
		return task, nil
	}
	if err := yaml.Unmarshal([]byte(raw), &task); err != nil {
		return task, fmt.Errorf("ax-runner: AX_TASK_YAML: %w", err)
	}
	return task, nil
}

// cmdAXRunner — подкоманда ax-runner.
func cmdAXRunner(args []string) {
	fsSet := flag.NewFlagSet("ax-runner", flag.ExitOnError)
	port := fsSet.Int("port", envInt("AGCTL_RUNNER_PORT", 80), "порт раннера (probes + прокси на dsh)")
	targetPort := fsSet.Int("target-port", envInt("DSH_PORT", 3080), "порт dsh web внутри песочницы")
	taskFile := fsSet.String("task-file", "", "файл Task вместо AX_TASK_YAML (локальная отладка)")
	home := fsSet.String("home", envOr("DSH_HOME", "/workspace/dsh-home"), "DSH_HOME на долговечном томе")
	work := fsSet.String("work", envOr("HOME", "/workspace/work"), "рабочий каталог пользователя (HOME, проекты)")
	_ = fsSet.Parse(args)

	raw := os.Getenv("AX_TASK_YAML")
	if *taskFile != "" {
		data, err := os.ReadFile(*taskFile)
		if err != nil {
			log.Fatal(err)
		}
		raw = string(data)
	}
	task, err := parseRunnerTask(raw)
	if err != nil {
		log.Fatal(err)
	}
	// spec.env AX уже выставил в окружение контейнера; для файлового режима —
	// применяем сами (не перекрывая явно заданное).
	for _, kv := range task.Spec.Env {
		if _, set := os.LookupEnv(kv.Name); !set && kv.Name != "" {
			_ = os.Setenv(kv.Name, kv.Value)
		}
	}
	_ = os.Setenv("DSH_HOME", *home)
	_ = os.Setenv("HOME", *work)
	for _, dir := range []string{*home, *work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	// Провайдер dsh: fetch-at-boot у шлюза (env задачи содержит только
	// AGCTL_GATEWAY_URL), иначе AGCTL_LLM_* окружения задачи. В режиме
	// fetch-at-boot недоступный шлюз — громкий fatal: тихий старт без
	// провайдера оставил бы dsh с пустым конфигом.
	taskName := task.Metadata.Name
	if taskName == "" {
		taskName = "unknown"
	}
	gwURL, gwToken := os.Getenv("AGCTL_GATEWAY_URL"), os.Getenv("AGCTL_RUNNER_TOKEN")
	provider := providerAtBoot(taskName, gwURL, gwToken)
	source := "env задачи"
	switch {
	case provider != nil:
		source = provider.source
	case gwURL != "":
		log.Fatalf("ax-runner: конфиг провайдера у шлюза %s недоступен (fetch-at-boot) — проверьте шлюз и токен раннера", gwURL)
	default:
		provider = &runnerProvider{Provider: providerFromEnv()}
	}
	log.Printf("ax-runner: провайдер %s (%s)", provider.Provider.Alias(), source)
	if err := prepareChatHome(*home, provider.Provider, true, os.Getenv("AGCTL_DEFAULT_FULL_ACCESS") == "1"); err != nil {
		log.Fatal(err)
	}
	command := task.Spec.Command
	if len(command) == 0 {
		command = defaultRunnerCommand
	}
	target := fmt.Sprintf("%s:%d", sandboxBindIP(), *targetPort)
	log.Printf("ax-runner: задача %s/%s, команда %v, dsh %s, порт %d", task.Metadata.Atespace, task.Metadata.Name, command, target, *port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sup := newSupervisor(command, *work, io.MultiWriter(os.Stdout))
	go sup.run(ctx)

	handler, guestCleanup := runnerHandler(sup, target, os.Getenv("AGCTL_RUNNER_TOKEN"), task.Spec.Debug, *work)
	if guestCleanup != nil {
		defer guestCleanup()
	}
	// HTTP/1.1 (probes/прокси) + h2c (gRPC guest-сервисов) на одном :80 —
	// паттерн раннера апстрима AX.
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(*port),
		Handler:           handler,
		Protocols:         &protocols,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	sup.wait()
}

// envInt — целое из env или дефолт.
func envInt(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return def
}

// sandboxBindIP — адрес, на котором dsh web слушает в песочнице (как
// agctl-dsh-net print: eth0, иначе первый не-loopback IPv4; fallback —
// 127.0.0.1).
func sandboxBindIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	pick := func(name string) string {
		for _, iface := range ifaces {
			if name != "" && iface.Name != name {
				continue
			}
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					return ipn.IP.String()
				}
			}
		}
		return ""
	}
	if ip := pick("eth0"); ip != "" {
		return ip
	}
	if ip := pick(""); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

// ── супервизор дочернего процесса ──────────────────────────────────────────

const (
	runnerLogLimit    = 256 << 10 // хвост вывода dsh в памяти
	runnerStopGrace   = 10 * time.Second
	runnerIODelay     = 2 * time.Second // ожидание вывода после выхода процесса
	runnerRestartMax  = 30 * time.Second
	runnerRestartBase = time.Second
)

// supervisor запускает команду, копирует её вывод в stdout и кольцевой буфер,
// перезапускает после выхода (раннер — PID 1, живёт всегда) и аккуратно
// останавливает по SIGTERM (TERM группе процессов, затем KILL).
type supervisor struct {
	command []string
	dir     string
	out     io.Writer

	mu      sync.Mutex
	buf     []byte
	cmd     *exec.Cmd
	stopped bool
	done    chan struct{}
}

func newSupervisor(command []string, dir string, out io.Writer) *supervisor {
	return &supervisor{command: command, dir: dir, out: out, done: make(chan struct{})}
}

// run — цикл запуска/перезапуска до отмены ctx.
func (s *supervisor) run(ctx context.Context) {
	defer close(s.done)
	backoff := runnerRestartBase
	for {
		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		log.Printf("ax-runner: команда завершилась (%v) — перезапуск через %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if time.Since(started) > time.Minute {
			backoff = runnerRestartBase
		} else if backoff < runnerRestartMax {
			backoff *= 2
		}
	}
}

// runOnce — один запуск команды до её выхода или остановки по ctx.
func (s *supervisor) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.command[0], s.command[1:]...)
	cmd.Dir = s.dir
	cmd.Env = os.Environ()
	setProcessGroup(cmd)
	// Внук (например sleep под sh) может пережить родителя и держать pipe
	// вывода: без WaitDelay cmd.Wait ждал бы его до конца.
	cmd.WaitDelay = runnerIODelay
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Bytes()
			s.append(line)
			_, _ = s.out.Write(append(append([]byte(nil), line...), '\n'))
		}
	}()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		err = s.terminate(cmd, waitErr)
	}
	_ = pw.Close()
	<-copyDone
	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()
	return err
}

// terminate — TERM группе процессов; не вышла за grace — KILL. После выхода
// лидера группу всё равно добиваем KILL: оставшиеся потомки (MCP, sleep)
// не должны пережить раннер. Возвращает результат cmd.Wait.
func (s *supervisor) terminate(cmd *exec.Cmd, waitErr <-chan error) error {
	signalGroup(cmd, syscall.SIGTERM)
	var err error
	select {
	case err = <-waitErr:
	case <-time.After(runnerStopGrace):
		signalGroup(cmd, syscall.SIGKILL)
		err = <-waitErr
	}
	signalGroup(cmd, syscall.SIGKILL)
	return err
}

// wait — дождаться выхода цикла супервизора.
func (s *supervisor) wait() { <-s.done }

// append — строка в кольцевой буфер (хвост runnerLogLimit байт).
func (s *supervisor) append(line []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, line...)
	s.buf = append(s.buf, '\n')
	if len(s.buf) > runnerLogLimit {
		cut := len(s.buf) - runnerLogLimit
		if nl := strings.IndexByte(string(s.buf[cut:]), '\n'); nl >= 0 {
			cut += nl + 1
		}
		s.buf = append([]byte(nil), s.buf[cut:]...)
	}
}

// tail — последние n строк вывода (n ≤ 0 = всё).
func (s *supervisor) tail(n int) string {
	s.mu.Lock()
	raw := string(s.buf)
	s.mu.Unlock()
	if n <= 0 {
		return raw
	}
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

// ── HTTP раннера ───────────────────────────────────────────────────────────

// runnerHandler — /healthz, /readyz, /agctl/boot-log (по токену), guest-
// сервисы Substrate (gRPC, только при spec.debug) и прокси остального на
// dsh web. Возвращает cleanup guest-сервера (nil, если debug выключен).
func runnerHandler(sup *supervisor, target, token string, debug bool, workDir string) (http.Handler, func()) {
	var guestSrv *grpc.Server
	var guestCleanup func()
	if debug {
		srv, cleanup, err := newGuestServer(workDir)
		if err != nil {
			log.Printf("ax-runner: guest-сервисы недоступны (ax ssh не заработает): %v", err)
		} else {
			guestSrv, guestCleanup = srv, cleanup
			log.Printf("ax-runner: guest-сервисы Substrate включены (spec.debug): process+filesystem в %s", workDir)
		}
	}
	targetURL := &url.URL{Scheme: "http", Host: target}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.Out.Host = target
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Ate-Target-Actor")
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "dsh недоступен: "+err.Error(), http.StatusBadGateway)
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		conn, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			http.Error(w, "dsh не слушает "+target, http.StatusServiceUnavailable)
			return
		}
		_ = conn.Close()
		_, _ = io.WriteString(w, "ready\n")
	})
	mux.HandleFunc("GET /agctl/boot-log", func(w http.ResponseWriter, r *http.Request) {
		if token == "" || !bearerMatches(r.Header.Get("Authorization"), token) {
			http.NotFound(w, r)
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, sup.tail(n))
	})
	mux.Handle("/", proxy)
	// gRPC guest-сервисов (h2c) — до прокси: ax ssh ходит через
	// маршрутизатор тем же :80, что и probes/прокси.
	var handler http.Handler = mux
	if guestSrv != nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				guestSrv.ServeHTTP(w, r)
				return
			}
			mux.ServeHTTP(w, r)
		})
	}
	return handler, guestCleanup
}

// bearerMatches — постоянное по времени сравнение Bearer-токена.
func bearerMatches(header, token string) bool {
	got := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// ── подготовка DSH_HOME (общая с init-chat-home) ───────────────────────────

// runnerProvider — платформенный провайдер для dsh: из fetch-at-boot
// (шлюз agctl по токену раннера), иначе из AGCTL_LLM_* окружения задачи.
type runnerProvider struct {
	provision.Provider
	source string
}

// providerAtBoot — конфиг провайдера у шлюза (GET /ax/provider-config/<task>
// с Bearer-токеном раннера); env задачи его не содержит. Ошибка или пустой
// ответ — nil: вызывающий падает громко (тихий fallback оставил бы dsh без
// провайдера или с протухшим).
func providerAtBoot(taskName, gatewayURL, token string) *runnerProvider {
	if gatewayURL == "" || token == "" {
		return nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	raw, err := axrt.FetchProviderConfig(client, gatewayURL, taskName, token)
	if err != nil {
		log.Printf("ax-runner: провайдер у шлюза недоступен: %v", err)
		return nil
	}
	var cfg axrt.ProviderConfigPayload
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("ax-runner: конфиг провайдера шлюза: %v", err)
		return nil
	}
	p := provision.Provider{
		Name:    cfg.Alias,
		BaseURL: firstString(cfg.BaseURLs),
		Model:   cfg.Model,
		Models:  cfg.Models,
		APIKey:  cfg.APIKey,
	}
	for _, extra := range cfg.Extras {
		x := provision.ExtraProvider{
			Name: extra.Name, BaseURL: extra.BaseURL, Model: extra.Model,
			Models: extra.Models, APIKey: extra.APIKey,
		}
		x.APIKeyEnv = x.KeyEnv()
		p.Extras = append(p.Extras, x)
	}
	return &runnerProvider{Provider: p, source: "gateway(fetch-at-boot)"}
}

// firstString — первый непустой элемент.
func firstString(vals []string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// providerFromEnv — платформенный провайдер из AGCTL_LLM_* окружения
// (K8s init-контейнер, раннер AX): ключи дополнительных провайдеров — из env
// по именам api_key_env.
func providerFromEnv() provision.Provider {
	models := []string{}
	if raw := os.Getenv("AGCTL_LLM_MODELS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			log.Fatalf("AGCTL_LLM_MODELS: %v", err)
		}
	}
	extras := []provision.ExtraProvider{}
	if raw := strings.TrimSpace(os.Getenv("AGCTL_LLM_EXTRA_PROVIDERS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &extras); err != nil {
			log.Fatalf("AGCTL_LLM_EXTRA_PROVIDERS: %v", err)
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
	return provision.Provider{
		Name:    os.Getenv("AGCTL_LLM_PROVIDER"),
		BaseURL: os.Getenv("AGCTL_LLM_BASE_URL"),
		Model:   os.Getenv("AGCTL_LLM_MODEL"),
		Models:  models,
		APIKey:  os.Getenv("AGCTL_LLM_API_KEY"),
		Extras:  extras,
	}
}

// prepareChatHome — DSH_HOME готов к старту dsh: скелет (пустой том),
// провайдер (refresh), плагины платформы, дефолт пресета разрешений — тот же
// набор шагов, что у Docker-адаптера на каждом ленивом старте.
func prepareChatHome(home string, provider provision.Provider, refresh, fullAccess bool) error {
	seeded, refreshed, err := initChatHome(home, provider, refresh, assets.DshSkel)
	if err != nil {
		return err
	}
	if err := provision.EnsureChatPlugins(home, assets.DshSkel); err != nil {
		return err
	}
	if err := provision.EnsurePermissionDefaults(home, fullAccess); err != nil {
		return fmt.Errorf("permission defaults: %w", err)
	}
	switch {
	case seeded:
		log.Printf("chat-home: seeded %s", home)
	case refreshed:
		log.Printf("chat-home: provider refreshed in %s", home)
	default:
		log.Printf("chat-home: existing home preserved %s", home)
	}
	return nil
}
