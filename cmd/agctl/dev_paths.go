package main

// dev_paths.go — `agctl dev paths`: витрина прозрачности локального режима.
// Одна команда отвечает на вопрос «что agctl держит на этой машине и где»:
// корень, бинари, профиль, дом и его файлы, порты, контейнеры, маунты,
// секреты. Ничего не меняет и значений секретов не читает.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/arijjia/agctl/internal/cli"
	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/server"
)

// pathItem — файл или каталог с назначением.
type pathItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
	Exists  bool   `json:"exists"`
}

// mountItem — bind-маунт host → container.
type mountItem struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	Purpose   string `json:"purpose"`
}

// containerItem — имя Docker-контейнера и его роль.
type containerItem struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

// pathsReport — всё, что печатает agctl dev paths (и отдаёт --json).
type pathsReport struct {
	Root struct {
		Path   string `json:"path"`
		Source string `json:"source"`
		Exists bool   `json:"exists"`
	} `json:"root"`
	Binaries   []pathItem `json:"binaries"`
	Executable string     `json:"executable"` // бинарь, который сейчас выполняется
	Guest      struct {
		Dir   string     `json:"dir"`
		Files []pathItem `json:"files"`
	} `json:"guest"`
	Profile struct {
		localmode.ProfileRef
		ProfilesDir string   `json:"profilesDir"`
		Available   []string `json:"available"` // профили в <корень>/profiles
		Error       string   `json:"error,omitempty"`
	} `json:"profile"`
	Home struct {
		ID          string     `json:"id"`
		Dir         string     `json:"dir"`
		Source      string     `json:"source"`
		Initialized bool       `json:"initialized"`
		Files       []pathItem `json:"files"`
		Others      []string   `json:"others"` // другие настроенные дома
	} `json:"home"`
	WorkDir string `json:"workdir"`
	UI      struct {
		Addr    string `json:"addr"`
		URL     string `json:"url"`
		Running bool   `json:"running"`
		PID     int    `json:"pid,omitempty"`
	} `json:"ui"`
	Bridge struct {
		Port    string `json:"port"`
		URL     string `json:"url"` // как мост виден из контейнера
		Listen  string `json:"listen"`
		Tools   int    `json:"tools"`
		Journal string `json:"journal"`
	} `json:"bridge"`
	Image struct {
		Ref      string `json:"ref"`
		Source   string `json:"source"`
		Previous string `json:"previous,omitempty"`
	} `json:"image"`
	Docker struct {
		Containers    []containerItem `json:"containers"`
		EgressNetwork string          `json:"egressNetwork"`
		EgressEnabled bool            `json:"egressEnabled"`
		Mounts        []mountItem     `json:"mounts"`
	} `json:"docker"`
	Secrets struct {
		Backend string   `json:"backend"`
		Service string   `json:"service,omitempty"` // сервис дома в хранилище ОС
		Legacy  string   `json:"legacy,omitempty"`  // прежний общий сервис (только чтение)
		File    string   `json:"file,omitempty"`    // зашифрованный файл (если хранилища ОС нет)
		Names   []string `json:"names"`             // имена из config.yaml; значений здесь нет
	} `json:"secrets"`
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func devPaths(h localmode.Home, args []string) error {
	fs := flag.NewFlagSet("dev paths", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "машинный вывод (JSON)")
	_ = fs.Parse(args)
	rep := devPathsReport(h)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(rep)
	}
	devPathsPrint(os.Stdout, rep)
	return nil
}

// devPathsReport собирает отчёт. Не требует настроенного дома: без конфига
// показывает раскладку и то, что появится после agctl dev init.
func devPathsReport(h localmode.Home) *pathsReport {
	rep := &pathsReport{}
	home := h.Dir
	rep.Root.Path, rep.Root.Source, rep.Root.Exists = localmode.Root(), localmode.RootSource(), exists(localmode.Root())

	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for _, b := range []struct{ name, purpose string }{
		{"agctl", "бинарь разработчика (локальный режим) — этот каталог должен быть в PATH"},
		{"agctl-server", "сервер организации; локальному режиму не нужен"},
	} {
		p := filepath.Join(localmode.BinDir(), b.name+exe)
		rep.Binaries = append(rep.Binaries, pathItem{Name: b.name, Path: p, Purpose: b.purpose, Exists: exists(p)})
	}
	if self, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(self); err == nil {
			self = r
		}
		rep.Executable = self
	}
	rep.Guest.Dir = localmode.GuestDir()
	for _, arch := range []string{"arm64", "amd64"} {
		for _, g := range []struct{ name, purpose string }{
			{"agctl-guest-linux-" + arch, "guest локального образа agctl-dsh* (мост к хосту), linux/" + arch},
			{"agctl-guest-server-linux-" + arch, "guest серверного образа agctl-dsh-server*, linux/" + arch},
		} {
			p := filepath.Join(rep.Guest.Dir, g.name)
			if exists(p) {
				rep.Guest.Files = append(rep.Guest.Files, pathItem{Name: g.name, Path: p, Purpose: g.purpose, Exists: true})
			}
		}
	}

	cfg, cfgErr := localmode.Load(home)
	rep.Home.ID, rep.Home.Dir, rep.Home.Source, rep.Home.Initialized = h.ID, home, h.Source, cfgErr == nil
	for _, id := range localmode.ListHomes() {
		if localmode.HomeDir(id) != home {
			rep.Home.Others = append(rep.Home.Others, id)
		}
	}
	if cfg == nil {
		cfg = &localmode.Config{Profile: "agctl", Addr: localmode.DefaultAddr}
	}
	rep.WorkDir = cfg.WorkDir

	rep.Profile.ProfilesDir, rep.Profile.Available = localmode.ProfilesDir(), localmode.DiskProfiles()
	prof, pref, perr := localmode.ResolveProfile(cfg)
	if perr != nil {
		rep.Profile.Error = perr.Error()
		rep.Profile.ID = cli.FirstNonEmpty(cfg.Profile, cfg.ProfileDir)
	} else {
		rep.Profile.ProfileRef = pref
	}

	data := localmode.ServerHome(home)
	owner := fmt.Sprintf("u%d", server.LocalOwnerID)
	dshHome := localmode.DshHome(home)
	configItem := pathItem{Name: localmode.ConfigName, Path: localmode.Path(home),
		Purpose: "настройки дома: профиль, образ, папка проектов, адрес, имена доступов (без значений)"}
	if !exists(configItem.Path) && exists(localmode.LegacyPath(home)) {
		configItem = pathItem{Name: localmode.LegacyConfigName, Path: localmode.LegacyPath(home),
			Purpose: "настройки дома под прежним именем; при первой записи станет config.yaml"}
	}
	files := []pathItem{
		configItem,
		{Name: "data/", Path: data, Purpose: "данные платформы этого дома (от пути зависят имена контейнеров)"},
		{Name: "data/agctl.db", Path: filepath.Join(data, "agctl.db"), Purpose: "SQLite: рабочие места и настройки лаунчера, сессии входа"},
		{Name: "data/secrets.key", Path: filepath.Join(data, "secrets.key"), Purpose: "мастер-ключ шифрования записей в SQLite (0600)"},
		{Name: "data/chats/" + owner + "/dsh-home/", Path: dshHome, Purpose: "домашний каталог агента: сессии, настройки, скиллы (в контейнере — /home/dsh)"},
		{Name: "logs/serve.log", Path: localmode.LogPath(home), Purpose: "лог фонового процесса (agctl dev logs)"},
		{Name: "logs/bridge.log", Path: localmode.BridgeJournalPath(home), Purpose: "журнал вызовов моста к хосту (agctl dev bridge log)"},
		{Name: "run/state.json", Path: localmode.StatePath(home), Purpose: "состояние запущенного процесса: pid, адрес, токены входа и управления (0600); есть, пока запущено"},
		{Name: "backups/", Path: localmode.BackupsDir(home), Purpose: fmt.Sprintf("бэкапы dsh-home перед обновлением образа (хранится %d)", localmode.KeepBackups)},
	}

	store := localmode.OpenStore(home)
	rep.Secrets.Backend = store.Backend()
	if store.Fallback() {
		rep.Secrets.File = store.FilePath()
		files = append(files,
			pathItem{Name: "secrets.enc", Path: store.FilePath(), Purpose: "секреты дома, зашифрованы AES-GCM (хранилище ОС недоступно или AGCTL_DEV_SECRETS=file)"},
			pathItem{Name: "secrets.key", Path: filepath.Join(home, "secrets.key"), Purpose: "ключ к secrets.enc (0600)"})
	} else {
		rep.Secrets.Service, rep.Secrets.Legacy = store.Service(), localmode.LegacyKeyringService
	}
	rep.Secrets.Names = append([]string{}, cfg.Secrets...)
	sort.Strings(rep.Secrets.Names)
	for i := range files {
		files[i].Exists = exists(files[i].Path)
	}
	rep.Home.Files = files

	rep.UI.Addr, rep.UI.URL = cfg.Addr, "http://"+cfg.Addr
	if st, _ := localmode.ReadState(home); st != nil && devAlive(st) {
		rep.UI.Running, rep.UI.PID = true, st.PID
		rep.UI.Addr, rep.UI.URL = st.Addr, "http://"+st.Addr
		rep.WorkDir = cli.FirstNonEmpty(st.WorkDir, rep.WorkDir)
	}
	_, port, _ := net.SplitHostPort(rep.UI.Addr)
	rep.Bridge.Port, rep.Bridge.URL = port, "http://host.docker.internal:"+port
	if runtime.GOOS == "linux" {
		rep.Bridge.Listen = "шлюз docker0:" + port + " — только пути моста (UI остаётся на loopback)"
	} else {
		rep.Bridge.Listen = "тот же слушатель, что и UI: Docker Desktop доводит host.docker.internal до loopback хоста"
	}
	rep.Bridge.Journal = localmode.BridgeJournalPath(home)
	if perr == nil {
		if tools, err := localmode.EffectiveBridgeTools(cfg, prof); err == nil {
			rep.Bridge.Tools = len(tools)
		}
	} else {
		rep.Bridge.Tools = len(cfg.Bridge.Tools)
	}

	switch {
	case cfg.Image != "":
		rep.Image.Ref, rep.Image.Source = cfg.Image, "config.yaml: image — ваш явный выбор"
	case perr == nil:
		rep.Image.Ref, rep.Image.Source = devDefaultImage(prof, cfg.Flavor), "профиль "+prof.ID+" — образ по умолчанию (в config.yaml не записан)"
	default:
		rep.Image.Source = "не определён: профиль не разрешён"
	}
	rep.Image.Previous = cfg.PreviousImage

	rep.Docker.Containers = []containerItem{
		{Name: server.ChatContainerName(data, server.LocalOwnerID, 0), Purpose: "основное рабочее место"},
		{Name: server.ChatContainerName(data, server.LocalOwnerID, 0) + "-s<N>", Purpose: "дополнительные рабочие места лаунчера (N = 1, 2, …)"},
	}
	rep.Docker.EgressNetwork, rep.Docker.EgressEnabled = dockerrt.EgressNetworkName(data), cfg.EgressAllowlist
	work := cli.FirstNonEmpty(rep.WorkDir, "(папка проектов не задана)")
	rep.Docker.Mounts = []mountItem{
		{Host: work, Container: "/workspace", Purpose: "папка проектов — всё, что агент видит на хосте"},
		{Host: dshHome, Container: "/home/dsh", Purpose: "домашний каталог агента (сессии, настройки, файл доступов 0600)"},
		{Host: filepath.Join(data, "chats", owner+"s<N>", "dsh-home"), Container: "/home/dsh", Purpose: "то же у дополнительного места N; его папки — <папка хоста> → /workspace/<имя>"},
	}
	return rep
}

// ── печать ─────────────────────────────────────────────────────────────────

func padRight(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "·"
}

func devPathsPrint(w io.Writer, r *pathsReport) {
	section := func(title string) { fmt.Fprintf(w, "\n%s\n", title) }
	kv := func(k, v string) { fmt.Fprintf(w, "  %s %s\n", padRight(k, 13), v) }
	note := func(s string) { fmt.Fprintf(w, "  %s %s\n", strings.Repeat(" ", 13), s) }
	items := func(list []pathItem) {
		nameW := 0
		for _, it := range list {
			nameW = max(nameW, utf8.RuneCountInString(it.Name))
		}
		for _, it := range list {
			fmt.Fprintf(w, "  %s %s  %s\n", mark(it.Exists), padRight(it.Name, nameW), it.Path)
			fmt.Fprintf(w, "    %s  %s\n", strings.Repeat(" ", nameW), it.Purpose)
		}
	}

	fmt.Fprintf(w, "agctl dev paths — что agctl держит на этой машине и где (дом «%s»)\n", r.Home.ID)
	fmt.Fprintln(w, "✓ — есть на диске, · — пока нет (появится по мере работы)")

	section("КОРЕНЬ")
	kv("путь", r.Root.Path)
	kv("выбран", r.Root.Source)
	note("bin/ — бинари · profiles/<id>/ — профили · homes/<id>/ — дома")

	section("БИНАРИ")
	items(r.Binaries)
	if r.Executable != "" && (len(r.Binaries) == 0 || r.Executable != r.Binaries[0].Path) {
		kv("сейчас", r.Executable+" — выполняется не из корня")
	}

	section("GUEST — бинари для сборки образа рабочего места на этой машине")
	kv("каталог", r.Guest.Dir)
	if len(r.Guest.Files) == 0 {
		note("пусто: образ можно только скачать из реестра (собрать — make install или установщик)")
	} else {
		items(r.Guest.Files)
	}

	section("ПРОФИЛЬ")
	if r.Profile.Error != "" {
		kv("ошибка", r.Profile.Error)
	} else {
		kv("id", r.Profile.ID)
		kv("путь", cli.FirstNonEmpty(r.Profile.Dir, "— (в бинаре, на диске нет)"))
		if r.Profile.Link != "" {
			kv("симлинк на", r.Profile.Link)
		}
		kv("разрешён", r.Profile.Origin)
	}
	note("порядок поиска: config.yaml profile_dir > " + filepath.Join(r.Profile.ProfilesDir, "<id>") + " > встроенный")
	if len(r.Profile.Available) > 0 {
		kv("в корне", strings.Join(r.Profile.Available, ", "))
	}

	section("ДОМ")
	kv("id", r.Home.ID)
	kv("путь", r.Home.Dir)
	kv("выбран", r.Home.Source)
	note("порядок выбора: --home <id> > AGCTL_DEV_HOME (путь) > AGCTL_HOME_ID > единственный дом > agctl")
	if !r.Home.Initialized {
		kv("состояние", "не настроен — agctl dev init")
	}
	if len(r.Home.Others) > 0 {
		kv("другие дома", strings.Join(r.Home.Others, ", ")+"  (agctl dev --home <id> paths)")
	}
	fmt.Fprintln(w)
	items(r.Home.Files)

	section("ПАПКА ПРОЕКТОВ")
	kv("путь", cli.FirstNonEmpty(r.WorkDir, "не задана — agctl dev up <папка>"))
	note("единственная папка хоста, которую видит агент")

	section("СЕТЬ")
	state := "остановлено"
	if r.UI.Running {
		state = fmt.Sprintf("запущено, pid %d", r.UI.PID)
	}
	kv("интерфейс", r.UI.URL+"  ("+state+"; только loopback)")
	kv("мост", r.Bridge.URL+"  — так его видит контейнер; порт "+r.Bridge.Port)
	note(r.Bridge.Listen)
	kv("инструменты", fmt.Sprintf("%d в белом списке моста (agctl dev bridge list)", r.Bridge.Tools))

	section("ОБРАЗ")
	kv("образ", cli.FirstNonEmpty(r.Image.Ref, "—"))
	kv("откуда", r.Image.Source)
	if r.Image.Previous != "" {
		kv("предыдущий", r.Image.Previous+"  (agctl dev rollback)")
	}

	section("DOCKER")
	nameW := 0
	for _, c := range r.Docker.Containers {
		nameW = max(nameW, utf8.RuneCountInString(c.Name))
	}
	for _, c := range r.Docker.Containers {
		fmt.Fprintf(w, "  контейнер     %s  %s\n", padRight(c.Name, nameW), c.Purpose)
	}
	egress := "выключен (egress_allowlist: false) — сеть не создаётся, контейнер в сети bridge"
	if r.Docker.EgressEnabled {
		egress = "включён — контейнер только в этой сети"
	}
	kv("сеть egress", r.Docker.EgressNetwork+"  "+egress)
	note("имена контейнеров и сети содержат хеш пути data/ — перенос дома их меняет")
	fmt.Fprintln(w, "  маунты (хост → контейнер):")
	for _, m := range r.Docker.Mounts {
		fmt.Fprintf(w, "    %s\n      → %s  %s\n", m.Host, padRight(m.Container, 10), m.Purpose)
	}

	section("СЕКРЕТЫ")
	kv("хранилище", r.Secrets.Backend)
	if r.Secrets.Service != "" {
		note("прежний общий сервис «" + r.Secrets.Legacy + "» только читается — если значения нет в сервисе дома")
	}
	names := "нет"
	if len(r.Secrets.Names) > 0 {
		names = strings.Join(r.Secrets.Names, ", ")
	}
	kv("имена", names+"  (значения — agctl dev secrets; в config.yaml и логах их нет)")
}
