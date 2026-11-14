package main

// dev.go — локальный режим (agctl dev): однопользовательская платформа на
// машине разработчика. agctl на хосте — мини-контроль-плейн: UI на loopback,
// контейнер рабочего места на локальном Docker, секреты в хранилище ОС, мост
// к хосту, обновления образа с бэкапом и откатом. Концепция — карточка
// local-mode-v2-container-bridge.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/server"
)

const devUsage = `agctl dev — локальный режим: платформа на этой машине (один пользователь)

  init      настроить: профиль, образ, папка проектов, доступы (Jira/GitLab/LLM)
  up [DIR]  поднять платформу и рабочее место (DIR → /workspace), открыть браузер
  down      остановить (данные и сессии сохраняются)
  status    состояние: адрес, образ, папка, рабочее место, мост
  open      открыть браузер (новая ссылка входа)
  logs [-f] лог фонового процесса
  secrets   доступы в хранилище ОС: list | set ИМЯ [--stdin] | rm ИМЯ
  bridge    мост к хосту: list | pending | approve ID | deny ID | log
  update    обновить образ рабочего места (бэкап, проверка, откат при сбое)
  rollback  вернуть предыдущий образ [--restore-data]
  doctor    самодиагностика: docker, образ, доступы, сеть, мост

Дом режима: $AGCTL_DEV_HOME (по умолчанию ~/.agctl-dev), конфиг — dev.yaml.
`

func cmdDev(args []string) {
	if len(args) == 0 {
		fmt.Print(devUsage)
		os.Exit(2)
	}
	home := localmode.Home()
	var err error
	switch args[0] {
	case "init":
		err = devInit(home, args[1:])
	case "up":
		err = devUp(home, args[1:])
	case "down":
		err = devDown(home)
	case "status":
		err = devStatus(home)
	case "open":
		err = devOpen(home, args[1:])
	case "logs":
		err = devLogs(home, args[1:])
	case "secrets":
		err = devSecrets(home, args[1:])
	case "bridge":
		err = devBridge(home, args[1:])
	case "update":
		err = devUpdate(home, args[1:])
	case "rollback":
		err = devRollback(home, args[1:])
	case "doctor":
		err = devDoctor(home)
	case "serve": // внутренняя: фоновый процесс, который запускает up
		devServe(home, args[1:])
		return
	case "help", "-h", "--help":
		fmt.Print(devUsage)
		return
	default:
		fmt.Fprintf(os.Stderr, "agctl dev: неизвестная команда %q\n\n%s", args[0], devUsage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "agctl dev %s: %v\n", args[0], err)
		os.Exit(1)
	}
}

// ── init ───────────────────────────────────────────────────────────────────

func devInit(home string, args []string) error {
	fs := flag.NewFlagSet("dev init", flag.ExitOnError)
	prof := fs.String("profile", "", "профиль организации (agctl, utmn…)")
	profDir := fs.String("profile-dir", "", "каталог профиля с диска")
	image := fs.String("image", "", "образ рабочего места (из реестра организации); пусто — по профилю")
	flavor := fs.String("flavor", "", "вкус образа из профиля (web, 1c…)")
	workdir := fs.String("workdir", "", "папка проектов по умолчанию (→ /workspace)")
	nonInteractive := fs.Bool("yes", false, "не спрашивать (только флаги)")
	noPull := fs.Bool("no-pull", false, "не тянуть образ сейчас")
	addr := fs.String("addr", "", "адрес UI (только 127.0.0.1:PORT), по умолчанию "+localmode.DefaultAddr)
	_ = fs.Parse(args)

	cfg, err := localmode.Load(home)
	if errors.Is(err, localmode.ErrNotInitialized) {
		cfg = &localmode.Config{}
	} else if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	ask := func(q, def string) string {
		if *nonInteractive {
			return def
		}
		if def != "" {
			fmt.Printf("%s [%s]: ", q, def)
		} else {
			fmt.Printf("%s: ", q)
		}
		line, _ := in.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
		return def
	}
	fmt.Printf("Локальный режим agctl. Дом: %s\n\n", home)
	if *prof != "" {
		cfg.Profile, cfg.ProfileDir = *prof, ""
	}
	if *profDir != "" {
		// Абсолютный путь: dev up / dev serve запускаются из любой папки.
		abs, err := filepath.Abs(*profDir)
		if err != nil {
			return err
		}
		if st, err := os.Stat(filepath.Join(abs, "profile.yaml")); err != nil || st.IsDir() {
			return fmt.Errorf("--profile-dir %s: нет profile.yaml", abs)
		}
		cfg.ProfileDir = abs
	}
	if cfg.ProfileDir == "" {
		cfg.Profile = ask("Профиль организации", firstNonEmpty(cfg.Profile, "agctl"))
	}
	p, err := devResolveProfile(cfg)
	if err != nil {
		return err
	}
	if *flavor != "" {
		cfg.Flavor = *flavor
	}
	if len(cfg.Bridge.Tools) == 0 {
		if tools, err := profileBridgeTools(p); err != nil {
			fmt.Printf("  ⚠ белый список моста из профиля: %v\n", err)
		} else if len(tools) > 0 {
			cfg.Bridge.Tools = tools
			fmt.Printf("Мост к хосту: из профиля взято инструментов — %d (правка — %s, раздел bridge.tools)\n", len(tools), localmode.Path(home))
		}
	}
	if *image != "" {
		cfg.Image = *image
	} else if cfg.Image == "" {
		cfg.Image = devDefaultImage(p, cfg.Flavor)
		if len(p.Images.Dev) > 1 && !*nonInteractive {
			fmt.Println("Образы профиля:")
			for i, ref := range p.Images.Dev {
				fmt.Printf("  %d) %s\n", i+1, ref)
			}
			if n, err := strconv.Atoi(ask("Номер образа", "1")); err == nil && n >= 1 && n <= len(p.Images.Dev) {
				cfg.Image = p.Images.Dev[n-1]
			}
		}
	}
	if *addr != "" {
		if !isLoopbackAddr(*addr) {
			return fmt.Errorf("--addr: только loopback (127.0.0.1:PORT), а не %s", *addr)
		}
		cfg.Addr = *addr
	}
	if *workdir != "" {
		cfg.WorkDir = *workdir
	} else {
		def := cfg.WorkDir
		if def == "" {
			if h, err := os.UserHomeDir(); err == nil {
				def = filepath.Join(h, "src")
			}
		}
		cfg.WorkDir = ask("Папка проектов (агент видит только её)", def)
	}
	if cfg.WorkDir != "" {
		if abs, err := localmode.ValidateWorkDir(cfg.WorkDir); err == nil {
			cfg.WorkDir = abs
		} else {
			fmt.Printf("  ⚠ %v\n", err)
		}
	}

	store := localmode.OpenStore(home)
	fmt.Printf("\nДоступы (хранилище: %s). Enter — пропустить/оставить.\n", store.Backend())
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for _, v := range localmode.Manifest {
		if *nonInteractive {
			break
		}
		if !v.Secret {
			val := ask(fmt.Sprintf("  %s (%s)", v.Title, v.Name), cfg.Env[v.Name])
			if val != "" {
				cfg.Env[v.Name] = val
			}
			continue
		}
		_, getErr := store.Get(v.Name)
		state := "не задан"
		if getErr == nil {
			state = "задан"
		}
		fmt.Printf("  %s (%s, %s)", v.Title, v.Name, state)
		if v.Hint != "" {
			fmt.Printf(" — %s", v.Hint)
		}
		fmt.Print(": ")
		val, err := readSecretLine(in)
		if err != nil {
			return err
		}
		if val == "" {
			if getErr == nil {
				cfg.Secrets = addName(cfg.Secrets, v.Name)
			}
			continue
		}
		if err := store.Set(v.Name, val); err != nil {
			return fmt.Errorf("%s: %w", v.Name, err)
		}
		cfg.Secrets = addName(cfg.Secrets, v.Name)
	}
	if err := cfg.Save(home); err != nil {
		return err
	}
	fmt.Printf("\n✓ настройки: %s\n", localmode.Path(home))

	if !*noPull && cfg.Image != "" {
		if err := devPull(cfg.Image); err != nil {
			fmt.Printf("  ⚠ образ %s: %v\n    Если образа нет в реестре, его соберёт agctl dev up (нужна сборка из исходников) — или выполните docker login в реестр организации и повторите agctl dev update.\n", cfg.Image, err)
		}
	}
	fmt.Println("\nДальше: agctl dev up   (или agctl dev up ~/src/проект)")
	return nil
}

func readSecretLine(in *bufio.Reader) (string, error) {
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		return strings.TrimSpace(string(b)), err
	}
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func addName(list []string, name string) []string {
	for _, n := range list {
		if n == name {
			return list
		}
	}
	return append(list, name)
}

func devResolveProfile(cfg *localmode.Config) (*profile.Data, error) {
	return profile.Resolve(firstNonEmpty(cfg.Profile, "agctl"), cfg.ProfileDir, "profiles", filepath.Join(localmode.Home(), "profiles"), filepath.Join(defaultHome(), "profiles"))
}

// devEnsureImage — образ рабочего места есть локально; нет — docker pull;
// нет и в реестре — собрать на этой машине всю цепочку (базовый → web →
// надстройка профиля → вкус профиля) из встроенного контекста, профиля и
// linux-сборки agctl рядом с бинарём. Первый раз — 10–20 минут.
func devEnsureImage(cfg *localmode.Config) error {
	p, err := devResolveProfile(cfg)
	if err != nil {
		return err
	}
	target := firstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor))
	ctx := context.Background()
	if _, err := dockerrt.ImageID(ctx, target); err == nil {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	fmt.Printf("… образа %s нет локально — пробую docker pull\n", target)
	if err := dockerrt.PullImage(pctx, target); err == nil {
		fmt.Printf("✓ образ %s скачан\n", target)
		return nil
	}
	b := dockerrt.DevBuild{
		Target: target, Flavor: devFlavorOf(cfg, p, target), BinDir: "dist",
		ImageFS: assets.Image, SkelFS: assets.DshSkel,
		Log: func(m string) { fmt.Println("  " + m) },
	}
	if p != nil {
		b.ProfileID = p.ID
		v := p.View()
		b.ProfileFS = v.FS
		if ov, df, ok := v.ImageContext(); ok {
			b.Overlay, b.OverlayDockerfile = ov, df
		}
	}
	fmt.Printf("… в реестре его тоже нет — собираю на этой машине: %s\n  (первый раз 10–20 минут: npm, apt, браузеры; дальше — из кэша Docker)\n",
		strings.Join(dockerrt.DevBuildPlan(b), " → "))
	bctx, bcancel := context.WithTimeout(ctx, 90*time.Minute)
	defer bcancel()
	if err := dockerrt.BuildDevImage(bctx, b); err != nil {
		return fmt.Errorf("сборка образа: %w", err)
	}
	return nil
}

// devFlavorOf — вкус образа: из dev.yaml, иначе по имени образа (-web, -<вкус профиля>).
func devFlavorOf(cfg *localmode.Config, p *profile.Data, image string) string {
	if cfg.Flavor != "" {
		return cfg.Flavor
	}
	if strings.Contains(image, "-web") {
		return "web"
	}
	if p != nil && p.View().FS != nil {
		if ents, err := fs.ReadDir(p.View().FS, "image/flavors"); err == nil {
			for _, e := range ents {
				if e.IsDir() && strings.HasSuffix(strings.SplitN(image, ":", 2)[0], "-"+e.Name()) {
					return e.Name()
				}
			}
		}
	}
	return ""
}

// devDefaultImage — образ по профилю: вкус (первое совпадение id в имени
// образа из images.dev), иначе первый из images.dev, иначе базовый.
func devDefaultImage(p *profile.Data, flavor string) string {
	if flavor != "" {
		for _, ref := range p.Images.Dev {
			if strings.Contains(ref, "-"+flavor) {
				return ref
			}
		}
	}
	if len(p.Images.Dev) > 0 {
		return p.Images.Dev[0]
	}
	if p.Images.Dockerfile != "" {
		return profile.OverlayImage("agctl-dsh:0.2.2", p.ID)
	}
	return "agctl-dsh:0.2.2"
}

func devPull(ref string) error {
	if _, err := dockerrt.ImageID(context.Background(), ref); err == nil {
		fmt.Printf("✓ образ %s уже есть локально\n", ref)
		return nil
	}
	fmt.Printf("… скачиваю образ %s (первый раз — несколько минут)\n", ref)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	if err := dockerrt.PullImage(ctx, ref); err != nil {
		return err
	}
	fmt.Printf("✓ образ %s скачан\n", ref)
	return nil
}

// ── up / serve / down ─────────────────────────────────────────────────────

func devUp(home string, args []string) error {
	fs := flag.NewFlagSet("dev up", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "не открывать браузер")
	foreground := fs.Bool("foreground", false, "в текущем терминале (Ctrl+C — остановка)")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	workdir := cfg.WorkDir
	if fs.NArg() > 0 {
		workdir = fs.Arg(0)
	}
	abs, err := localmode.ValidateWorkDir(workdir)
	if err != nil {
		return err
	}
	if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
		if st.WorkDir != abs {
			return fmt.Errorf("уже запущено с папкой %s — сначала agctl dev down", st.WorkDir)
		}
		fmt.Printf("✓ уже запущено: http://%s\n", st.Addr)
		if !*noOpen {
			return devOpen(home, nil)
		}
		return nil
	}
	if _, err := dockerrt.PingDaemon(context.Background()); err != nil {
		return err
	}
	if err := devEnsureImage(cfg); err != nil {
		return err
	}
	if ln, err := net.Listen("tcp", cfg.Addr); err != nil {
		return fmt.Errorf("адрес %s занят (другой экземпляр agctl dev? agctl dev down; иначе addr в %s): %w", cfg.Addr, localmode.Path(home), err)
	} else {
		_ = ln.Close()
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	serveArgs := []string{"dev", "serve", "--workdir", abs}
	if *foreground {
		fmt.Printf("agctl dev: папка %s, UI http://%s (Ctrl+C — остановка)\n", abs, cfg.Addr)
		go func() {
			if st := devWaitReady(home, 3*time.Minute); st != nil && !*noOpen {
				_ = openBrowser(devLoginURL(st))
			}
		}()
		devServe(home, serveArgs[2:])
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(localmode.LogPath(home)), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(localmode.LogPath(home), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	localmode.ClearState(home)
	cmd := exec.Command(self, serveArgs...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = logf, logf, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск фонового процесса: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	fmt.Printf("… поднимаю платформу (папка %s)\n", abs)
	st := devWaitReadyOrExit(home, 3*time.Minute, exited)
	if st == nil {
		select {
		case <-exited:
			return fmt.Errorf("фоновый процесс завершился при старте — хвост лога (%s):\n%s", localmode.LogPath(home), tailFile(localmode.LogPath(home), 8))
		default:
		}
		return fmt.Errorf("платформа не поднялась за 3 минуты — смотрите agctl dev logs (%s)", localmode.LogPath(home))
	}
	fmt.Printf("✓ agctl dev: http://%s  (образ %s)\n", st.Addr, st.Image)
	fmt.Println("  остановка: agctl dev down · состояние: agctl dev status · лог: agctl dev logs")
	if *noOpen {
		fmt.Printf("  вход: %s\n", devLoginURL(st))
		return nil
	}
	if err := openBrowser(devLoginURL(st)); err != nil {
		fmt.Printf("  откройте в браузере: %s\n", devLoginURL(st))
	}
	return nil
}

func devLoginURL(st *localmode.State) string {
	return "http://" + st.Addr + "/local/login?token=" + st.LoginToken
}

// devWaitReadyOrExit — как devWaitReady, но прекращает ждать, если фоновый
// процесс завершился.
func devWaitReadyOrExit(home string, timeout time.Duration, exited <-chan struct{}) *localmode.State {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
			return st
		}
		select {
		case <-exited:
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

func tailFile(path string, n int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// devWaitReady ждёт файл состояния и ответ /api/local/status.
func devWaitReady(home string, timeout time.Duration) *localmode.State {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
			return st
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func devControl(st *localmode.State, method, path string, out any) error {
	req, err := http.NewRequest(method, "http://"+st.Addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+st.ControlToken)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func devAlive(st *localmode.State) bool {
	return devControl(st, http.MethodGet, "/api/local/status", nil) == nil
}

// devServe — фоновый процесс: читает dev.yaml и секреты, поднимает сервер в
// локальном режиме, пишет файл состояния.
func devServe(home string, args []string) {
	fs := flag.NewFlagSet("dev serve", flag.ExitOnError)
	workdirFlag := fs.String("workdir", "", "папка проектов")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		log.Fatal(err)
	}
	workdir, err := localmode.ValidateWorkDir(firstNonEmpty(*workdirFlag, cfg.WorkDir))
	if err != nil {
		log.Fatal(err)
	}
	store := localmode.OpenStore(home)
	env, missing := localmode.ContainerEnv(cfg, store)
	if len(missing) > 0 {
		log.Printf("dev: доступы не заданы: %s (agctl dev secrets set …)", strings.Join(missing, ", "))
	}
	// Изоляция от серверного режима: ни ~/.config/agctl/config.yaml, ни env
	// серверного запуска (.env make) не влияют на локальный.
	_ = os.Setenv("AGCTL_CONFIG", filepath.Join(home, "serve-config.yaml"))
	for _, k := range []string{"AGCTL_PROFILE", "AGCTL_PROFILE_DIR", "AGCTL_ADDR", "AGCTL_DEV_ENV", "AGCTL_DEV_IMAGES", "AGCTL_HOME"} {
		_ = os.Unsetenv(k)
	}
	if key, err := store.Get("AGCTL_LLM_API_KEY"); err == nil && key != "" {
		_ = os.Setenv("AGCTL_LLM_API_KEY", key)
	} else {
		log.Printf("dev: ключ модели не задан — agctl dev secrets set AGCTL_LLM_API_KEY")
	}
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		log.Fatalf("dev: addr %q: %v", cfg.Addr, err)
	}
	if !isLoopbackAddr(cfg.Addr) {
		log.Fatalf("dev: локальный режим слушает только loopback, а не %s", cfg.Addr)
	}
	local := &server.LocalMode{
		OwnerLogin:   "owner",
		WorkDir:      workdir,
		LoginToken:   localmode.NewToken(),
		ControlToken: localmode.NewToken(),
		BridgeToken:  localmode.NewToken(),
		BridgeURL:    "http://host.docker.internal:" + port,
		BridgeTools:  cfg.Bridge.Tools,
		BridgeLog:    localmode.BridgeJournalPath(home),
		Env:          env,
	}
	serveArgs := []string{
		"--home", localmode.ServerHome(home),
		"--addr", cfg.Addr,
		"--mem-gb", strconv.FormatFloat(cfg.MemGB, 'f', -1, 64),
		"--cpus", strconv.FormatFloat(cfg.CPUs, 'f', -1, 64),
		"--chat-ttl-min", "720", // рабочий день: не усыплять контейнер на обеде
		"--dev-env=",
	}
	if cfg.ProfileDir != "" {
		serveArgs = append(serveArgs, "--profile-dir", cfg.ProfileDir)
	} else {
		serveArgs = append(serveArgs, "--profile", firstNonEmpty(cfg.Profile, "agctl"))
	}
	image := cfg.Image
	if image != "" {
		serveArgs = append(serveArgs, "--image", image, "--dev-images", image)
	}
	onReady := func(ctx context.Context, h http.Handler) {
		devBridgeListener(ctx, port, h)
		st := localmode.State{
			PID: os.Getpid(), Addr: cfg.Addr, ControlToken: local.ControlToken, LoginToken: local.LoginToken,
			Image: firstNonEmpty(image, "по профилю"), WorkDir: workdir, Started: time.Now(), Version: version,
		}
		if err := localmode.WriteState(home, st); err != nil {
			log.Printf("dev: файл состояния: %v", err)
		}
		go func() {
			<-ctx.Done()
			localmode.ClearOwnState(home, os.Getpid())
		}()
	}
	cmdServeWith(serveArgs, local, onReady)
	localmode.ClearOwnState(home, os.Getpid())
}

// devBridgeListener: на Linux (Docker Engine) host.docker.internal указывает
// на шлюз docker0, а не на 127.0.0.1 — мосту нужен слушатель там. Отдаём на
// нём ТОЛЬКО пути моста (вход и UI остаются на loopback). На Docker Desktop
// (macOS/Windows) host.docker.internal доходит до loopback хоста сам.
func devBridgeListener(ctx context.Context, port string, h http.Handler) {
	if runtime.GOOS != "linux" {
		return
	}
	ip := dockerGatewayIP()
	if ip == "" {
		log.Printf("dev: шлюз docker0 не найден — мост к хосту из контейнера может быть недоступен")
		return
	}
	only := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/local/bridge/") || strings.HasPrefix(r.URL.Path, "/api/local/bridge/pending") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: net.JoinHostPort(ip, port), Handler: only, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dev: мост на %s: %v", srv.Addr, err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	log.Printf("dev: мост к хосту для контейнеров: %s", srv.Addr)
}

func dockerGatewayIP() string {
	iface, err := net.InterfaceByName("docker0")
	if err != nil {
		return ""
	}
	addrs, _ := iface.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	return ""
}

func devDown(home string) error {
	st, err := localmode.ReadState(home)
	if err != nil {
		return err
	}
	if st == nil || !devAlive(st) {
		localmode.ClearState(home)
		fmt.Println("✓ не запущено")
		return nil
	}
	if err := devControl(st, http.MethodPost, "/api/local/shutdown", nil); err != nil {
		return err
	}
	fmt.Print("… останавливаю (рабочее место сохраняется)")
	for i := 0; i < 240; i++ {
		time.Sleep(500 * time.Millisecond)
		if !devAlive(st) {
			fmt.Println("\n✓ остановлено")
			localmode.ClearState(home)
			return nil
		}
		if i%4 == 0 {
			fmt.Print(".")
		}
	}
	return errors.New("процесс не остановился за 2 минуты — проверьте agctl dev logs")
}

// ── status / open / logs ──────────────────────────────────────────────────

func devStatus(home string) error {
	cfg, cfgErr := localmode.Load(home)
	st, _ := localmode.ReadState(home)
	if cfgErr != nil {
		return cfgErr
	}
	fmt.Printf("дом:      %s\n", home)
	fmt.Printf("профиль:  %s\n", firstNonEmpty(cfg.ProfileDir, cfg.Profile))
	fmt.Printf("образ:    %s\n", firstNonEmpty(cfg.Image, "по профилю"))
	fmt.Printf("папка:    %s\n", firstNonEmpty(cfg.WorkDir, "(не задана — agctl dev up DIR)"))
	if st == nil || !devAlive(st) {
		fmt.Println("статус:   остановлено (agctl dev up)")
		return nil
	}
	var s struct {
		Version       string         `json:"version"`
		Image         string         `json:"image"`
		Workdir       string         `json:"workdir"`
		Runtime       map[string]any `json:"runtime"`
		BridgeTools   int            `json:"bridgeTools"`
		BridgePending int            `json:"bridgePending"`
		Profile       string         `json:"profile"`
	}
	if err := devControl(st, http.MethodGet, "/api/local/status", &s); err != nil {
		return err
	}
	fmt.Printf("статус:   запущено, http://%s (pid %d, с %s)\n", st.Addr, st.PID, st.Started.Format("15:04 02.01"))
	fmt.Printf("рабочее:  %v, папка %s\n", s.Runtime["state"], s.Workdir)
	fmt.Printf("мост:     инструментов %d, ждут подтверждения %d\n", s.BridgeTools, s.BridgePending)
	return nil
}

func devOpen(home string, _ []string) error {
	st, _ := localmode.ReadState(home)
	if st == nil || !devAlive(st) {
		return errors.New("не запущено — agctl dev up")
	}
	url := devLoginURL(st)
	if err := openBrowser(url); err != nil {
		fmt.Printf("откройте в браузере: %s\n", url)
	}
	return nil
}

func devLogs(home string, args []string) error {
	follow := len(args) > 0 && (args[0] == "-f" || args[0] == "--follow")
	f, err := os.Open(localmode.LogPath(home))
	if err != nil {
		return err
	}
	defer f.Close()
	info, _ := f.Stat()
	if info.Size() > 32<<10 {
		_, _ = f.Seek(-32<<10, io.SeekEnd)
	}
	_, _ = io.Copy(os.Stdout, f)
	for follow {
		time.Sleep(time.Second)
		_, _ = io.Copy(os.Stdout, f)
	}
	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ── secrets ────────────────────────────────────────────────────────────────

func devSecrets(home string, args []string) error {
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	store := localmode.OpenStore(home)
	if len(args) == 0 || args[0] == "list" {
		fmt.Printf("хранилище: %s\n\n", store.Backend())
		names := map[string]bool{}
		for _, v := range localmode.Manifest {
			names[v.Name] = true
		}
		for _, n := range cfg.Secrets {
			names[n] = true
		}
		for n := range cfg.Env {
			names[n] = true
		}
		sorted := make([]string, 0, len(names))
		for n := range names {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)
		for _, n := range sorted {
			v, known := localmode.ManifestVar(n)
			title := ""
			if known {
				title = " — " + v.Title
			}
			if val, ok := cfg.Env[n]; ok {
				fmt.Printf("  %-28s %s%s\n", n, val, title)
				continue
			}
			state := "—"
			if _, err := store.Get(n); err == nil {
				state = "задан (скрыт)"
			}
			fmt.Printf("  %-28s %s%s\n", n, state, title)
		}
		return nil
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("dev secrets set", flag.ExitOnError)
		stdin := fs.Bool("stdin", false, "значение из stdin (для скриптов)")
		plain := fs.Bool("plain", false, "несекретное значение (URL, логин) — в dev.yaml")
		if len(args) < 2 {
			return errors.New("использование: agctl dev secrets set ИМЯ [--stdin] [--plain]")
		}
		name := args[1]
		_ = fs.Parse(args[2:])
		if !localmode.ValidEnvName(name) {
			return fmt.Errorf("имя %q: только A-Z, 0-9, _", name)
		}
		if v, ok := localmode.ManifestVar(name); ok && !v.Secret {
			*plain = true
		}
		in := bufio.NewReader(os.Stdin)
		var val string
		if *stdin {
			line, _ := in.ReadString('\n')
			val = strings.TrimSpace(line)
		} else {
			fmt.Printf("%s: ", name)
			if *plain {
				line, _ := in.ReadString('\n')
				val = strings.TrimSpace(line)
			} else if val, err = readSecretLine(in); err != nil {
				return err
			}
		}
		if val == "" {
			return errors.New("пустое значение — для удаления: agctl dev secrets rm ИМЯ")
		}
		if *plain {
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			cfg.Env[name] = val
		} else {
			if err := store.Set(name, val); err != nil {
				return err
			}
			cfg.Secrets = addName(cfg.Secrets, name)
		}
		if err := cfg.Save(home); err != nil {
			return err
		}
		fmt.Printf("✓ %s сохранён; применится при следующем запуске (agctl dev down && agctl dev up)\n", name)
		return nil
	case "rm":
		if len(args) < 2 {
			return errors.New("использование: agctl dev secrets rm ИМЯ")
		}
		name := args[1]
		if err := store.Delete(name); err != nil {
			return err
		}
		delete(cfg.Env, name)
		out := cfg.Secrets[:0]
		for _, n := range cfg.Secrets {
			if n != name {
				out = append(out, n)
			}
		}
		cfg.Secrets = out
		if err := cfg.Save(home); err != nil {
			return err
		}
		fmt.Printf("✓ %s удалён\n", name)
		return nil
	}
	return errors.New("agctl dev secrets list | set ИМЯ | rm ИМЯ")
}

// ── bridge ─────────────────────────────────────────────────────────────────

func devBridge(home string, args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		cfg, err := localmode.Load(home)
		if err != nil {
			return err
		}
		if len(cfg.Bridge.Tools) == 0 {
			fmt.Printf("Белый список пуст. Добавьте инструменты в %s, раздел bridge.tools (пример — docs/local-mode.md).\n", localmode.Path(home))
			return nil
		}
		for _, t := range cfg.Bridge.Tools {
			conf := ""
			if t.Confirm {
				conf = " [подтверждение]"
			}
			fmt.Printf("  %-24s %s%s\n      %s\n", t.Name, t.Description, conf, strings.Join(t.Command, " "))
		}
		return nil
	case "log":
		raw, err := os.ReadFile(localmode.BridgeJournalPath(home))
		if err != nil {
			return err
		}
		os.Stdout.Write(raw)
		return nil
	case "pending", "approve", "deny":
		st, _ := localmode.ReadState(home)
		if st == nil || !devAlive(st) {
			return errors.New("не запущено — agctl dev up")
		}
		if sub == "pending" {
			var out struct {
				Pending []localbridge.Pending `json:"pending"`
			}
			if err := devControl(st, http.MethodGet, "/api/local/bridge/pending", &out); err != nil {
				return err
			}
			if len(out.Pending) == 0 {
				fmt.Println("нет вызовов, ждущих подтверждения")
			}
			for _, p := range out.Pending {
				fmt.Printf("  %s  %s  %s\n", p.ID, p.Tool, strings.Join(p.Argv, " "))
			}
			return nil
		}
		if len(args) < 2 {
			return fmt.Errorf("использование: agctl dev bridge %s ID", sub)
		}
		if err := devControl(st, http.MethodPost, "/api/local/bridge/pending/"+args[1]+"/"+sub, nil); err != nil {
			return err
		}
		fmt.Printf("✓ %s: %s\n", args[1], map[string]string{"approve": "подтверждён", "deny": "отклонён"}[sub])
		return nil
	}
	return errors.New("agctl dev bridge list | pending | approve ID | deny ID | log")
}

// ── update / rollback ──────────────────────────────────────────────────────

func devUpdate(home string, args []string) error {
	fs := flag.NewFlagSet("dev update", flag.ExitOnError)
	image := fs.String("image", "", "целевой образ; пусто — текущий тег заново из реестра (новая сборка под тем же тегом) или образ профиля")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	p, err := devResolveProfile(cfg)
	if err != nil {
		return err
	}
	current := firstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor))
	target := firstNonEmpty(*image, current)
	ctx := context.Background()
	oldID, _ := dockerrt.ImageID(ctx, current)

	fmt.Printf("1/5 проверка: текущий %s → целевой %s\n", current, target)
	fmt.Println("2/5 скачивание образа")
	if err := dockerrt.PullImage(ctx, target); err != nil {
		// Образ, собранный локально (make img_build) или подтянутый раньше,
		// реестру может быть неизвестен — тогда берём локальный, честно.
		if _, lerr := dockerrt.ImageID(ctx, target); lerr != nil {
			return fmt.Errorf("%w (ничего не изменено)", err)
		}
		fmt.Printf("    ⚠ реестр не отдал %s (%v) — беру локальный образ\n", target, err)
	}
	newID, _ := dockerrt.ImageID(ctx, target)
	if target == current && newID == oldID && oldID != "" {
		fmt.Println("✓ уже актуально — обновлять нечего")
		return nil
	}
	st, _ := localmode.ReadState(home)
	wasRunning := st != nil && devAlive(st)
	workdir := cfg.WorkDir
	if wasRunning {
		workdir = st.WorkDir
		fmt.Println("    останавливаю платформу")
		if err := devDown(home); err != nil {
			return err
		}
	}
	fmt.Println("3/5 бэкап сессий и настроек dsh")
	backup, err := localmode.Backup(home, "before-"+target)
	if err != nil {
		return err
	}
	if backup != "" {
		fmt.Printf("    %s\n", backup)
	}
	cfg.PreviousImage = current
	cfg.Image = target
	if err := cfg.Save(home); err != nil {
		return err
	}
	fmt.Println("4/5 запуск на новом образе (управляемые слои dsh перезасеваются, ваши — не трогаются)")
	if err := devUp(home, []string{"--no-open", workdir}); err != nil {
		return devAutoRollback(home, cfg, current, backup, err)
	}
	fmt.Println("5/5 проверка рабочего места")
	if err := devVerify(home); err != nil {
		return devAutoRollback(home, cfg, current, backup, err)
	}
	fmt.Printf("✓ обновлено: %s (откат — agctl dev rollback)\n", target)
	if !wasRunning {
		return devDown(home)
	}
	return nil
}

// devVerify — рабочее место поднимается на новом образе (не просто «процесс
// запустился»: контейнер стартует и отвечает).
func devVerify(home string) error {
	st, _ := localmode.ReadState(home)
	if st == nil {
		return errors.New("нет файла состояния")
	}
	req, _ := http.NewRequest(http.MethodGet, devLoginURL(st), nil)
	cl := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		var s struct {
			Runtime map[string]any `json:"runtime"`
		}
		if err := devControl(st, http.MethodGet, "/api/local/status", &s); err == nil && s.Runtime["state"] == "running" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("рабочее место не поднялось за 5 минут")
}

func devAutoRollback(home string, cfg *localmode.Config, previous, backup string, cause error) error {
	fmt.Printf("✗ сбой: %v\n↩ откат на %s\n", cause, previous)
	_ = devDown(home)
	if backup != "" {
		if err := localmode.Restore(home, backup); err != nil {
			fmt.Printf("  ⚠ восстановление данных: %v (архив цел: %s)\n", err, backup)
		}
	}
	cfg.Image, cfg.PreviousImage = previous, ""
	if err := cfg.Save(home); err != nil {
		return err
	}
	return fmt.Errorf("обновление не удалось, возвращён %s: %w", previous, cause)
}

func devRollback(home string, args []string) error {
	fs := flag.NewFlagSet("dev rollback", flag.ExitOnError)
	restore := fs.Bool("restore-data", false, "также вернуть сессии/настройки dsh из последнего бэкапа")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	if cfg.PreviousImage == "" {
		return errors.New("откатываться не на что: предыдущий образ не записан")
	}
	st, _ := localmode.ReadState(home)
	wasRunning := st != nil && devAlive(st)
	workdir := cfg.WorkDir
	if wasRunning {
		workdir = st.WorkDir
		if err := devDown(home); err != nil {
			return err
		}
	}
	if *restore {
		backups := localmode.Backups(home)
		if len(backups) == 0 {
			return errors.New("бэкапов нет")
		}
		if err := localmode.Restore(home, backups[0]); err != nil {
			return err
		}
		fmt.Printf("✓ данные dsh восстановлены из %s\n", backups[0])
	}
	cfg.Image, cfg.PreviousImage = cfg.PreviousImage, cfg.Image
	if err := cfg.Save(home); err != nil {
		return err
	}
	fmt.Printf("✓ образ: %s\n", cfg.Image)
	if wasRunning {
		return devUp(home, []string{"--no-open", workdir})
	}
	return nil
}

// ── doctor ─────────────────────────────────────────────────────────────────

func devDoctor(home string) error {
	ok := true
	check := func(name string, err error, okMsg string) {
		if err != nil {
			ok = false
			fmt.Printf("✗ %-22s %v\n", name, err)
			return
		}
		fmt.Printf("✓ %-22s %s\n", name, okMsg)
	}
	cfg, err := localmode.Load(home)
	check("настройки", err, localmode.Path(home))
	if err != nil {
		return errors.New("сначала agctl dev init")
	}
	api, err := dockerrt.PingDaemon(context.Background())
	check("docker", err, "API "+api)
	p, perr := devResolveProfile(cfg)
	check("профиль", perr, firstNonEmpty(cfg.ProfileDir, cfg.Profile))
	if err == nil && perr == nil {
		ref := firstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor))
		_, ierr := dockerrt.ImageID(context.Background(), ref)
		if ierr != nil {
			ierr = fmt.Errorf("%s нет локально — agctl dev update (или соберётся при up)", ref)
		}
		check("образ", ierr, ref)
	}
	if cfg.WorkDir != "" {
		_, werr := localmode.ValidateWorkDir(cfg.WorkDir)
		check("папка проектов", werr, cfg.WorkDir)
	}
	store := localmode.OpenStore(home)
	fmt.Printf("· %-22s %s\n", "хранилище доступов", store.Backend())
	for _, v := range localmode.Manifest {
		if !v.Secret {
			continue
		}
		_, gerr := store.Get(v.Name)
		if gerr != nil {
			fmt.Printf("· %-22s %s не задан (agctl dev secrets set %s)\n", v.Group, v.Name, v.Name)
		} else {
			fmt.Printf("✓ %-22s %s задан\n", v.Group, v.Name)
		}
	}
	if perr == nil {
		if base := strings.TrimSpace(p.LLM.DefaultBaseURL); base != "" {
			check("сеть: модель", devReach(base), base)
		}
	}
	for _, k := range []string{"GITLAB_URL", "JIRA_URL"} {
		if u := cfg.Env[k]; u != "" {
			check("сеть: "+strings.ToLower(strings.TrimSuffix(k, "_URL")), devReach(u), u)
		}
	}
	if err := localbridge.Validate(cfg.Bridge.Tools); err != nil {
		check("мост", err, "")
	} else {
		check("мост", nil, fmt.Sprintf("инструментов в белом списке: %d", len(cfg.Bridge.Tools)))
	}
	if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
		check("платформа", nil, "запущена: http://"+st.Addr)
	} else {
		fmt.Printf("· %-22s остановлена (agctl dev up)\n", "платформа")
	}
	if !ok {
		return errors.New("есть проблемы (✗ выше)")
	}
	return nil
}

// devReach — достижимость URL (любой HTTP-ответ = сеть есть).
func devReach(url string) error {
	cl := &http.Client{Timeout: 8 * time.Second}
	resp, err := cl.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// profileBridgeTools — белый список моста по умолчанию из профиля
// организации (local/bridge-tools.yaml, ключ tools). Нет файла — пусто.
func profileBridgeTools(p *profile.Data) ([]localbridge.Tool, error) {
	fsys := p.View().FS
	if fsys == nil {
		return nil, nil
	}
	raw, err := fs.ReadFile(fsys, "local/bridge-tools.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Tools []localbridge.Tool `yaml:"tools"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if err := localbridge.Validate(doc.Tools); err != nil {
		return nil, err
	}
	return doc.Tools, nil
}
