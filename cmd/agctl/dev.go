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

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/cli"
	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/runtime/sbxrt"
	"github.com/arijjia/agctl/internal/server"
	"golang.org/x/term"
)

const devUsage = `agctl dev — локальный режим: платформа на этой машине (один пользователь)

  init      настроить дом: профиль и папка проектов (образ и доступы — в лаунчере; --cli — в терминале)
  up [DIR]  поднять платформу и рабочее место (DIR → /workspace), открыть браузер
            --runtime docker|sbx — рантайм рабочего места (init и up; запоминается в dev.yaml)
  down      остановить (данные и сессии сохраняются)
  status    состояние: адрес, образ, папка, рабочее место, мост
  paths     где что лежит: корень, бинари, профиль, дом и его файлы, порты, контейнеры, маунты, секреты [--json]
  images    образы каталога: есть ли на машине, версия dsh, сборка guest, дата, размер, устаревшие [--json]
  open      открыть браузер (новая ссылка входа)
  logs [-f] лог фонового процесса
  secrets   доступы в хранилище ОС: list | set ИМЯ [--stdin] | rm ИМЯ
  bridge    мост к хосту: list | pending | approve ID | deny ID | log
  update    обновить образ рабочего места (бэкап, проверка, откат при сбое)
  rollback  вернуть предыдущий образ [--restore-data]
  doctor    самодиагностика: docker, рантайм (sbx), образ, доступы, сеть, мост
  migrate   перенести прежние дома ~/.agctl-dev[-<id>] в <корень>/homes/<id> (план; --apply — выполнить)

Всё лежит под одним корнем — ~/.agctl (переменная AGCTL_ROOT):
  bin/            бинари agctl, agctl-server; bin/guest/ — guest для сборки образа
  profiles/<id>/  профиль организации
  homes/<id>/     дом: config.yaml, data/, logs/, run/, backups/

Дом выбирается так: agctl dev --home <id> … > AGCTL_HOME_ID > единственный
существующий дом > agctl. Подробно — agctl dev paths.
`

func cmdDev(args []string) {
	homeFlag, args, err := devSplitHomeFlag(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agctl dev: %v\n", err)
		os.Exit(2)
	}
	if len(args) == 0 {
		fmt.Print(devUsage)
		os.Exit(2)
	}
	h, err := localmode.SelectHome(homeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agctl dev: %v\n", err)
		os.Exit(2)
	}
	home := h.Dir
	switch args[0] {
	case "init":
		err = devInit(h, args[1:])
	case "up":
		if err = devAutoInit(h, args[1:]); err == nil {
			err = devUp(home, args[1:])
		}
	case "down":
		err = devDown(home)
	case "status":
		err = devStatus(home)
	case "paths":
		err = devPaths(h, args[1:])
	case "images":
		err = devImages(home, args[1:])
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
	case "migrate":
		err = devMigrate(args[1:])
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
		if errors.Is(err, localmode.ErrNotInitialized) {
			if ids := localmode.ListHomes(); len(ids) > 0 && h.Source == localmode.HomeFromDefault {
				fmt.Fprintf(os.Stderr, "  дом «%s» (%s) не настроен; настроенные дома: %s — выберите: agctl dev --home <id> %s\n",
					h.ID, h.Dir, strings.Join(ids, ", "), args[0])
			}
		}
		os.Exit(1)
	}
}

// devSplitHomeFlag вынимает глобальный флаг выбора дома (--home <id>,
// --home=<id>) из аргументов dev: он действует для любой подкоманды и может
// стоять до или после неё.
func devSplitHomeFlag(args []string) (string, []string, error) {
	id := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--home" || a == "-home":
			if i+1 >= len(args) {
				return "", nil, errors.New("--home: нужен id дома (agctl dev --home <id> …)")
			}
			id = args[i+1]
			i++
		case strings.HasPrefix(a, "--home="):
			id = strings.TrimPrefix(a, "--home=")
		case strings.HasPrefix(a, "-home="):
			id = strings.TrimPrefix(a, "-home=")
		default:
			rest = append(rest, a)
		}
	}
	return id, rest, nil
}

// ── init ───────────────────────────────────────────────────────────────────

func devInit(h localmode.Home, args []string) error {
	fs := flag.NewFlagSet("dev init", flag.ExitOnError)
	prof := fs.String("profile", "", "профиль организации: id из <корень>/profiles (agctl — встроенный нейтральный)")
	profDir := fs.String("profile-dir", "", "явный каталог профиля вместо <корень>/profiles/<id>")
	image := fs.String("image", "", "образ рабочего места (из реестра организации); пусто — по профилю")
	flavor := fs.String("flavor", "", "вкус образа: web, min или вкус профиля организации")
	workdir := fs.String("workdir", "", "папка проектов по умолчанию (→ /workspace)")
	nonInteractive := fs.Bool("yes", false, "не спрашивать (только флаги)")
	noPull := fs.Bool("no-pull", false, "не тянуть образ сейчас (с --cli)")
	cliWizard := fs.Bool("cli", false, "спросить образ и доступы в терминале (по умолчанию — в браузере, в лаунчере)")
	addr := fs.String("addr", "", "адрес UI (только 127.0.0.1:PORT), по умолчанию "+localmode.DefaultAddr)
	runtimeFlag := fs.String("runtime", "", "рантайм рабочего места: docker (по умолчанию) | sbx (Docker Sandboxes)")
	hostPaths := fs.Bool("host-paths", runtime.GOOS != "windows", "пути как на хосте: папки проектов видны в контейнере по тем же путям (новый дом — включено, кроме Windows)")
	_ = fs.Parse(args)
	hostPathsSet := false
	fs.Visit(func(f *flag.Flag) { hostPathsSet = hostPathsSet || f.Name == "host-paths" })

	h = devInitHome(h, *prof, *profDir)
	home := h.Dir
	cfg, err := localmode.Load(home)
	if errors.Is(err, localmode.ErrNotInitialized) {
		// Новый дом: «пути как на хосте» по умолчанию (агент и вы видите
		// одни и те же пути); прежние дома не меняются без явного флага.
		cfg = &localmode.Config{HostPaths: *hostPaths && runtime.GOOS != "windows"}
	} else if err != nil {
		return err
	} else if hostPathsSet {
		cfg.HostPaths = *hostPaths && runtime.GOOS != "windows"
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
	fmt.Printf("Локальный режим agctl. Дом «%s»: %s (%s)\n\n", h.ID, home, h.Source)
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
		def := cfg.Profile
		if def == "" {
			// Единственный профиль в <корень>/profiles (так оставляет
			// установщик комплекта) — разумный дефолт: init без флагов
			// подхватывает организацию.
			def = devDefaultProfile(localmode.ProfilesDir())
		}
		cfg.Profile = ask("Профиль организации", def)
	}
	p, pref, err := localmode.ResolveProfile(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Профиль: %s — %s\n", p.ID, devProfileWhere(pref))
	if *flavor != "" {
		cfg.Flavor = *flavor
	}
	// Из профиля в дом ничего не копируется: белый список моста профиля
	// читается на каждом старте, образ по умолчанию — тоже из профиля.
	if tools, err := localmode.ProfileBridgeTools(p); err != nil {
		fmt.Printf("  ⚠ белый список моста профиля: %v\n", err)
	} else if len(tools) > 0 {
		fmt.Printf("Мост к хосту: инструментов профиля — %d (читаются из профиля на каждом старте; личные — %s, раздел bridge.tools)\n", len(tools), localmode.Path(home))
	}
	if *image != "" {
		cfg.Image = *image
	} else if cfg.Image == "" {
		// В config.yaml — только явный выбор; без него действует образ профиля.
		if len(p.Images.Dev) > 1 && !*nonInteractive && *cliWizard {
			fmt.Println("Образы профиля:")
			for i, ref := range p.Images.Dev {
				fmt.Printf("  %d) %s\n", i+1, ref)
			}
			if n, err := strconv.Atoi(ask("Номер образа", "1")); err == nil && n >= 1 && n <= len(p.Images.Dev) {
				cfg.Image = p.Images.Dev[n-1]
			}
		}
	}
	if *runtimeFlag != "" {
		kind, err := localmode.ParseRuntime(*runtimeFlag)
		if err != nil {
			return fmt.Errorf("--runtime: %w", err)
		}
		cfg.Runtime = kind
		if kind == localmode.RuntimeSbx {
			if version, err := sbxrt.Probe(context.Background(), ""); err != nil {
				fmt.Printf("  ⚠ рантайм sbx выбран, но пока недоступен: %v\n", err)
			} else {
				fmt.Printf("Рантайм рабочего места: sbx (%s)\n", version)
			}
		}
	}
	if *addr != "" {
		if !cli.IsLoopbackAddr(*addr) {
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
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if *cliWizard {
		fmt.Printf("\nДоступы (хранилище: %s). Enter — пропустить/оставить.\n", store.Backend())
	}
	for _, v := range localmode.Manifest {
		if *nonInteractive || !*cliWizard {
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

	if ref := cli.FirstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor)); *cliWizard && !*noPull && ref != "" {
		if err := devPull(ref); err != nil {
			fmt.Printf("  ⚠ образ %s: %v\n    Если образа нет в реестре, его соберёт лаунчер (agctl dev up → «Запустить») — или выполните docker login в реестр организации и повторите agctl dev update.\n", ref, err)
		}
	}
	up := "agctl dev up"
	if ids := localmode.ListHomes(); len(ids) > 1 || h.Source == localmode.HomeFromFlag {
		up = "agctl dev --home " + h.ID + " up"
	}
	if *cliWizard {
		fmt.Printf("\nДальше: %s   (или … up ~/src/проект); где что лежит — agctl dev paths\n", up)
	} else {
		fmt.Printf("\nДальше: %s — откроется лаунчер: там образ рабочего места и доступы (ключ модели, GitLab, Jira). Где что лежит — agctl dev paths.\n", up)
	}
	return nil
}

// devInitHome — дом для init. Если дом не выбран явно (флаг, переменные,
// единственный существующий) и дом по умолчанию ещё не настроен, а профиль
// задан флагом, id дома = id профиля: один дом на организацию.
func devInitHome(h localmode.Home, profID, profDir string) localmode.Home {
	if h.Source != localmode.HomeFromDefault {
		return h
	}
	if _, err := localmode.Load(h.Dir); !errors.Is(err, localmode.ErrNotInitialized) {
		return h
	}
	id := strings.TrimSpace(profID)
	if profDir != "" {
		if p, err := profile.LoadDir(profDir); err == nil {
			id = p.ID
		}
	}
	if id == "" || id == h.ID || !localmode.ValidID(id) {
		return h
	}
	return localmode.Home{ID: id, Dir: localmode.HomeDir(id), Source: "по профилю " + id}
}

// devProfileWhere — человекочитаемое «где лежит профиль и чем разрешён».
func devProfileWhere(ref localmode.ProfileRef) string {
	switch {
	case ref.Dir == "":
		return ref.Origin
	case ref.Link != "":
		return fmt.Sprintf("%s → %s (%s)", ref.Dir, ref.Link, ref.Origin)
	}
	return fmt.Sprintf("%s (%s)", ref.Dir, ref.Origin)
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

// devResolveProfile — профиль дома. Порядок один для CLI и serve
// (localmode.ResolveProfile): profile_dir из config.yaml >
// <корень>/profiles/<id> > встроенный.
func devResolveProfile(cfg *localmode.Config) (*profile.Data, error) {
	p, _, err := localmode.ResolveProfile(cfg)
	return p, err
}

// devDefaultProfile — имя профиля по умолчанию для мастера init: если в
// <корень>/profiles лежит ровно один профиль организации (так
// оставляет установщик комплекта), он и есть разумный дефолт; иначе —
// нейтральный agctl.
func devDefaultProfile(dataDir string) string {
	ents, err := os.ReadDir(dataDir)
	if err != nil {
		return "agctl"
	}
	found := ""
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dataDir, e.Name(), "profile.yaml")); err != nil {
			continue
		}
		if found != "" {
			return "agctl" // несколько организаций — выбор за пользователем
		}
		found = e.Name()
	}
	if found == "" {
		return "agctl"
	}
	return found
}

func devPrepareImage(ctx context.Context, p *profile.Data, target, flavor string, logf func(string)) error {
	pctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	logf(fmt.Sprintf("… образа %s нет локально — пробую docker pull", target))
	pullErr := dockerrt.PullImage(pctx, target)
	if pullErr == nil {
		logf(fmt.Sprintf("✓ образ %s скачан", target))
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Образ реестра собирается на месте, только когда реестр сказал «нет
	// такого» (или пакет закрыт); сбой связи — ошибка: иначе 10–20 минут
	// сборки, и образ под именем реестра уже не скачается.
	if dockerrt.LocalImageName(target) != target && !dockerrt.PullMissing(pullErr) {
		return fmt.Errorf("образ %s не скачан: %v — похоже на сбой связи с реестром (сеть, VPN, прокси, docker login); повторите, когда связь будет, или выберите локальный образ (agctl dev images)", target, pullErr)
	}
	b := dockerrt.DevBuild{
		Target: target, Flavor: flavor, BinDir: localmode.GuestDir(),
		ImageFS: assets.Image, SkelFS: assets.DshSkel,
		Version: version, Commit: commit, BuildDate: buildDate,
		Log: func(m string) { logf("  " + m) },
	}
	if p != nil {
		b.ProfileID = p.ID
		v := p.View()
		b.ProfileFS = v.FS
		if ov, df, ok := v.ImageContext(); ok {
			b.Overlay, b.OverlayDockerfile = ov, df
		}
	}
	logf(fmt.Sprintf("… в реестре его тоже нет — собираю на этой машине: %s (первый раз 10–20 минут: npm, apt, браузеры; дальше — из кэша Docker)",
		strings.Join(dockerrt.DevBuildPlan(b), " → ")))
	bctx, bcancel := context.WithTimeout(ctx, 90*time.Minute)
	defer bcancel()
	if err := dockerrt.BuildDevImage(bctx, b); err != nil {
		return fmt.Errorf("сборка образа: %w", err)
	}
	return nil
}

// devFlavorOf — вкус образа: из config.yaml, иначе по имени образа (-web, -<вкус профиля>).
func devFlavorOf(cfg *localmode.Config, p *profile.Data, image string) string {
	if cfg.Flavor != "" {
		return cfg.Flavor
	}
	if strings.Contains(image, "-web") {
		return "web"
	}
	if strings.HasSuffix(strings.SplitN(image[strings.LastIndex(image, "/")+1:], ":", 2)[0], "agctl-dsh-min") {
		return "min"
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

// devUpFlags — флаги agctl dev up (общие для up и авто-init первого up).
func devUpFlags(handling flag.ErrorHandling) (fs *flag.FlagSet, noOpen, foreground *bool, runtimeFlag *string) {
	fs = flag.NewFlagSet("dev up", handling)
	noOpen = fs.Bool("no-open", false, "не открывать браузер")
	foreground = fs.Bool("foreground", false, "в текущем терминале (Ctrl+C — остановка)")
	runtimeFlag = fs.String("runtime", "", "рантайм рабочего места: docker | sbx (запоминается в dev.yaml)")
	return fs, noOpen, foreground, runtimeFlag
}

func devUp(home string, args []string) error {
	fs, noOpen, foreground, runtimeFlag := devUpFlags(flag.ExitOnError)
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	runtimeChanged := false
	if *runtimeFlag != "" {
		kind, err := localmode.ParseRuntime(*runtimeFlag)
		if err != nil {
			return fmt.Errorf("--runtime: %w", err)
		}
		if runtimeChanged = kind != cfg.RuntimeKind(); runtimeChanged {
			if err := localmode.SetRuntime(home, kind); err != nil {
				return err
			}
			cfg.Runtime = kind
		}
	}
	if _, err := localmode.ParseRuntime(cfg.Runtime); err != nil {
		return fmt.Errorf("%s: %w", localmode.Path(home), err)
	}
	if cfg.RuntimeKind() == localmode.RuntimeSbx {
		// Явная ошибка до старта, а не при первом открытии рабочего места.
		if _, err := sbxrt.Probe(context.Background(), ""); err != nil {
			return fmt.Errorf("рантайм sbx: %w\n  вернуть Docker: agctl dev up --runtime docker", err)
		}
	}
	workdir := cfg.WorkDir
	if fs.NArg() > 0 {
		workdir = fs.Arg(0)
	}
	abs, err := localmode.ValidateWorkDir(workdir)
	if err != nil {
		return err
	}
	if st, _ := localmode.ReadState(home); st != nil && devAlive(st) && devStale(st) {
		// Бинарь обновили (install.sh, release), а фоновый процесс — ещё
		// старой сборки: без перезапуска фиксы новой версии не применятся.
		fmt.Printf("… запущена другая сборка agctl (%s, установлена %s) — перезапускаю\n", devStateBuild(st), devBuildID())
		if err := devDown(home); err != nil {
			return fmt.Errorf("перезапуск старой сборки: %w", err)
		}
	}
	if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
		if st.WorkDir != abs {
			return fmt.Errorf("уже запущено с папкой %s — сначала agctl dev down", st.WorkDir)
		}
		fmt.Printf("✓ уже запущено: http://%s\n", st.Addr)
		if runtimeChanged {
			fmt.Printf("  рантайм рабочего места: %s — применится при перезапуске места (лаунчер → «Перезапустить»)\n", cfg.RuntimeKind())
		}
		if !*noOpen {
			return devOpen(home, nil)
		}
		return nil
	}
	if _, err := dockerrt.PingDaemon(context.Background()); err != nil {
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
	// Фоновый процесс работает ровно с этим домом, как бы его ни выбрали здесь.
	cmd.Env = append(os.Environ(), "AGCTL_DEV_HOME="+home)
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

// devBuildID — сборка этого бинаря: версия и коммит (у локальных сборок
// версия часто одна — "dev", различает коммит).
func devBuildID() string { return buildID(version, commit) }

func buildID(v, c string) string {
	if c == "" {
		return v
	}
	return v + " " + c
}

// devStateBuild — сборка запущенного процесса; у состояний до поля build —
// только версия.
func devStateBuild(st *localmode.State) string {
	return cli.FirstNonEmpty(st.Build, st.Version)
}

// devStale — фоновый процесс запущен другой сборкой, чем этот бинарь.
func devStale(st *localmode.State) bool {
	if st.Build != "" {
		return st.Build != devBuildID()
	}
	return st.Version != "" && st.Version != version
}

func devAlive(st *localmode.State) bool {
	return devControl(st, http.MethodGet, "/api/local/status", nil) == nil
}

// devForeignEnv — переменные окружения серверного режима, которые не должны
// влиять на локальный: их снимает dev serve до того, как выставит свои из
// дома. Сюда попадает всё, что make экспортирует из .env репозитория
// (AGCTL_LLM_*, AGCTL_EGRESS_*, адреса и CORS сервера организации).
func devForeignEnv(environ []string) []string {
	exact := map[string]bool{
		"AGCTL_PROFILE": true, "AGCTL_PROFILE_DIR": true, "AGCTL_ADDR": true,
		"AGCTL_DEV_ENV": true, "AGCTL_DEV_IMAGES": true, "AGCTL_HOME": true,
		"AGCTL_PUBLIC_URL": true, "AGCTL_CORS_ORIGINS": true, "AGCTL_CONFIG": true,
	}
	var out []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if exact[k] || strings.HasPrefix(k, "AGCTL_LLM_") || strings.HasPrefix(k, "AGCTL_EGRESS_") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// devServe — фоновый процесс: читает config.yaml и секреты, поднимает сервер в
// локальном режиме, пишет файл состояния.
func devServe(home string, args []string) {
	fs := flag.NewFlagSet("dev serve", flag.ExitOnError)
	workdirFlag := fs.String("workdir", "", "папка проектов")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		log.Fatal(err)
	}
	workdir, err := localmode.ValidateWorkDir(cli.FirstNonEmpty(*workdirFlag, cfg.WorkDir))
	if err != nil {
		log.Fatal(err)
	}
	// Изоляция от серверного режима: ни ~/.config/agctl/config.yaml, ни env
	// серверного запуска (.env make) не влияют на локальный. Снимаем ДО того,
	// как выставим значения дома (ключи доп. провайдеров могут называться
	// AGCTL_LLM_*).
	for _, k := range devForeignEnv(os.Environ()) {
		_ = os.Unsetenv(k)
	}
	_ = os.Setenv("AGCTL_CONFIG", filepath.Join(home, "serve-config.yaml"))
	store := localmode.OpenStore(home)
	env, missing := localmode.ContainerEnv(cfg, store)
	if len(missing) > 0 {
		log.Printf("dev: доступы не заданы: %s (agctl dev secrets set …)", strings.Join(missing, ", "))
	}
	// Ключи доп. провайдеров профиля — в env этого процесса: cli.Serve читает
	// их через os.Getenv по api_key_env профиля (хранилище ОС ему не видно).
	// В env контейнера они не попадают: рабочему месту уходит только
	// ContainerEnv, а он ключи моделей пропускает. Ключи личных провайдеров
	// сервер берёт через хуки лаунчера.
	for k, v := range localmode.ModelKeys(cfg, store, devProfileKeyEnvs(cfg)...) {
		if k != "AGCTL_LLM_API_KEY" {
			_ = os.Setenv(k, v)
		}
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
	if !cli.IsLoopbackAddr(cfg.Addr) {
		log.Fatalf("dev: локальный режим слушает только loopback, а не %s", cfg.Addr)
	}
	// Профиль разрешается один раз и уходит в serve готовым: порядок поиска
	// один для CLI и сервера (localmode.ResolveProfile).
	prof, pref, err := localmode.ResolveProfile(cfg)
	if err != nil {
		log.Fatalf("dev: %v", err)
	}
	log.Printf("dev: дом %s (%s); профиль %s — %s", localmode.HomeID(home), home, prof.ID, devProfileWhere(pref))
	// Белый список моста: профиль (читается сейчас) + личные из config.yaml.
	tools, err := localmode.EffectiveBridgeTools(cfg, prof)
	if err != nil {
		log.Printf("dev: белый список моста профиля не читается: %v — действуют только личные инструменты из config.yaml", err)
		tools = cfg.Bridge.Tools
	}
	local := &server.LocalMode{
		OwnerLogin:   "owner",
		WorkDir:      workdir,
		LoginToken:   localmode.NewToken(),
		ControlToken: localmode.NewToken(),
		BridgeToken:  localmode.NewToken(),
		BridgeURL:    "http://host.docker.internal:" + port,
		BridgeTools:  tools,
		BridgeLog:    localmode.BridgeJournalPath(home),
		Env:          env,
		Hooks:        &devHooks{home: home, store: store, prof: prof},
	}
	// Образ: явный выбор из config.yaml, иначе образ профиля (в конфиг не
	// копируется — смена images.dev в профиле действует со следующего старта).
	image := cli.FirstNonEmpty(cfg.Image, devDefaultImage(prof, cfg.Flavor))
	serveArgs := []string{
		"--home", localmode.ServerHome(home),
		"--addr", cfg.Addr,
		"--mem-gb", strconv.FormatFloat(cfg.MemGB, 'f', -1, 64),
		"--cpus", strconv.FormatFloat(cfg.CPUs, 'f', -1, 64),
		"--chat-ttl-min", "720", // рабочий день: не усыплять контейнер на обеде
		"--dev-env=",
		"--bin-dir", localmode.GuestDir(), // guest для сборки образа — <корень>/bin/guest, не ./dist
		"--image", image, "--dev-images", image,
	}
	serveArgs = append(serveArgs, devEgressArgs(cfg)...)
	onReady := func(ctx context.Context, h http.Handler) {
		devBridgeListener(ctx, port, h)
		st := localmode.State{
			PID: os.Getpid(), Addr: cfg.Addr, ControlToken: local.ControlToken, LoginToken: local.LoginToken,
			Image: image, WorkDir: workdir, Started: time.Now(), Version: version, Build: devBuildID(),
		}
		if err := localmode.WriteState(home, st); err != nil {
			log.Printf("dev: файл состояния: %v", err)
		}
		go func() {
			<-ctx.Done()
			localmode.ClearOwnState(home, os.Getpid())
		}()
	}
	cli.Serve(serveArgs, cli.ServeOptions{Local: local, OnReady: onReady, Profile: prof, Runtime: devRuntimeFlags(home)})
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
	fmt.Printf("дом:      %s (%s)\n", localmode.HomeID(home), home)
	image := cli.FirstNonEmpty(cfg.Image, "по профилю")
	if p, pref, err := localmode.ResolveProfile(cfg); err != nil {
		fmt.Printf("профиль:  ✗ %v\n", err)
	} else {
		fmt.Printf("профиль:  %s — %s\n", p.ID, devProfileWhere(pref))
		if cfg.Image == "" {
			image = devDefaultImage(p, cfg.Flavor) + " (по профилю)"
		}
	}
	fmt.Printf("образ:    %s\n", image)
	fmt.Printf("рантайм:  %s\n", cfg.RuntimeKind())
	fmt.Printf("папка:    %s\n", cli.FirstNonEmpty(cfg.WorkDir, "(не задана — agctl dev up DIR)"))
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
	if devStale(st) {
		fmt.Printf("сборка:   запущена %s, установлена %s — перезапуск: agctl dev down && agctl dev up\n", devStateBuild(st), devBuildID())
	} else {
		fmt.Printf("сборка:   %s\n", devBuildID())
	}
	if kind, ok := s.Runtime["kind"].(string); ok && kind != "" {
		fmt.Printf("рабочее:  %v (%s), папка %s\n", s.Runtime["state"], kind, s.Workdir)
	} else {
		fmt.Printf("рабочее:  %v, папка %s\n", s.Runtime["state"], s.Workdir)
	}
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
		plain := fs.Bool("plain", false, "несекретное значение (URL, логин) — в config.yaml")
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
		p, err := devResolveProfile(cfg)
		if err != nil {
			return err
		}
		fromProfile, err := localmode.ProfileBridgeTools(p)
		if err != nil {
			fmt.Printf("⚠ белый список моста профиля не читается: %v\n", err)
		}
		tools := localmode.MergeBridgeTools(fromProfile, cfg.Bridge.Tools)
		if len(tools) == 0 {
			fmt.Printf("Белый список пуст. Личные инструменты — %s, раздел bridge.tools; инструменты организации — local/bridge-tools.yaml профиля (описание — docs/architecture.md, раздел 6.6).\n", localmode.Path(home))
			return nil
		}
		for _, t := range tools {
			conf := ""
			if t.Confirm {
				conf = " [подтверждение]"
			}
			src := "профиль " + p.ID
			if t.Personal {
				src = "config.yaml"
			}
			fmt.Printf("  %-24s %s%s  (%s)\n      %s\n", t.Name, t.Description, conf, src, strings.Join(t.Command, " "))
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
	current := cli.FirstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor))
	target := cli.FirstNonEmpty(*image, current)
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
	// В config.yaml — только явный выбор: обновление «того же тега по
	// профилю» оставляет image пустым (образ и дальше следует профилю).
	explicit := cfg.Image
	cfg.PreviousImage = current
	if *image != "" || explicit != "" {
		cfg.Image = target
	}
	if err := cfg.Save(home); err != nil {
		return err
	}
	fmt.Println("4/5 запуск на новом образе (управляемые слои dsh перезасеваются, ваши — не трогаются)")
	if err := devUp(home, []string{"--no-open", workdir}); err != nil {
		return devAutoRollback(home, cfg, explicit, current, backup, err)
	}
	fmt.Println("5/5 проверка рабочего места")
	if err := devVerify(home); err != nil {
		return devAutoRollback(home, cfg, explicit, current, backup, err)
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

// devAutoRollback возвращает прежний образ: explicit — что стояло в
// config.yaml до обновления (пусто = образ по профилю), previous — сам образ.
func devAutoRollback(home string, cfg *localmode.Config, explicit, previous, backup string, cause error) error {
	fmt.Printf("✗ сбой: %v\n↩ откат на %s\n", cause, previous)
	_ = devDown(home)
	if backup != "" {
		if err := localmode.Restore(home, backup); err != nil {
			fmt.Printf("  ⚠ восстановление данных: %v (архив цел: %s)\n", err, backup)
		}
	}
	cfg.Image, cfg.PreviousImage = explicit, ""
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
	check("корень", nil, localmode.Root()+" — "+localmode.RootSource())
	check("настройки", err, localmode.Path(home))
	if err != nil {
		return errors.New("сначала agctl dev init")
	}
	api, err := dockerrt.PingDaemon(context.Background())
	check("docker", err, "API "+api)
	kind, kerr := localmode.ParseRuntime(cfg.Runtime)
	check("рантайм", kerr, kind)
	sbxVersion, sbxErr := sbxrt.Probe(context.Background(), "")
	switch {
	case kind == localmode.RuntimeSbx:
		// установлен, вход выполнен, виртуализация (Apple Silicon / KVM) есть
		check("sbx", sbxErr, sbxVersion)
	case sbxErr != nil:
		fmt.Printf("· %-22s не выбран; %v\n", "sbx", sbxErr)
	default:
		fmt.Printf("· %-22s %s — доступен, включить: agctl dev up --runtime sbx\n", "sbx", sbxVersion)
	}
	p, pref, perr := localmode.ResolveProfile(cfg)
	check("профиль", perr, devProfileWhere(pref))
	if err == nil && perr == nil {
		ref := cli.FirstNonEmpty(cfg.Image, devDefaultImage(p, cfg.Flavor))
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
	if perr == nil {
		if tools, err := localmode.EffectiveBridgeTools(cfg, p); err != nil {
			check("мост", err, "")
		} else {
			check("мост", nil, fmt.Sprintf("инструментов в белом списке: %d (личных в config.yaml: %d, остальные — из профиля)", len(tools), len(cfg.Bridge.Tools)))
		}
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

// devEgressArgs — egress-allowlist из config.yaml в флаги serve. Явно и в обе
// стороны: серверные env AGCTL_EGRESS_* машины на локальный режим не влияют.
func devEgressArgs(cfg *localmode.Config) []string {
	return []string{
		"--egress-allowlist=" + strconv.FormatBool(cfg.EgressAllowlist),
		"--egress-allow=" + strings.Join(cfg.EgressAllow, ","),
	}
}

// devProfileKeyEnvs — имена переменных с ключами доп. провайдеров профиля
// (llm.extra_providers[].api_key_env).
func devProfileKeyEnvs(cfg *localmode.Config) []string {
	prof, err := devResolveProfile(cfg)
	if err != nil || prof == nil {
		return nil
	}
	var out []string
	for _, p := range prof.LLM.ExtraProviders {
		if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
			out = append(out, env)
		}
	}
	return out
}

// devAutoInit — первый `agctl dev up` на машине без единого дома: дом по
// умолчанию создаётся сам (как `agctl dev init --yes`), папка проектов —
// аргумент up или ~/src (создаётся). Образ, доступы и папки дальше — в
// лаунчере. Если дома уже есть, ничего не делает: выбор дома остаётся явным.
func devAutoInit(h localmode.Home, upArgs []string) error {
	if _, err := localmode.Load(h.Dir); !errors.Is(err, localmode.ErrNotInitialized) {
		return nil
	}
	if len(localmode.ListHomes()) > 0 {
		return nil // дома есть, но не этот — пусть ошибка скажет, какой выбрать
	}
	fs, _, _, _ := devUpFlags(flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_ = fs.Parse(upArgs) // ошибки флагов скажет сам up
	workdir := fs.Arg(0)
	if workdir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		workdir = filepath.Join(home, "src")
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return fmt.Errorf("папка проектов %s: %w", workdir, err)
	}
	fmt.Printf("… дом «%s» ещё не настроен — настраиваю по умолчанию (как agctl dev init --yes), папка проектов %s\n", h.ID, workdir)
	return devInit(h, []string{"--yes", "--workdir", workdir})
}
