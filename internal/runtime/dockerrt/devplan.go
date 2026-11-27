package dockerrt

// devplan.go — что войдёт в образ локального режима, если собирать его на
// этой машине: ступени цепочки DevBuildPlan, у каждой — Dockerfile (текст),
// build-arg'и, контекст сборки (что это за файлы и откуда они берутся) и что
// качается из сети (базовые образы, пакеты apt и npm, загрузки). Для
// лаунчера («Образы» → «Как собирается») и интроспекции; собирает
// BuildDevImage — тем же порядком ступеней и с теми же аргументами.

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/arijjia/agctl/internal/assets"
)

// DevStage — ступень сборки образа.
type DevStage struct {
	Ref        string            `json:"ref"`
	Kind       string            `json:"kind"`   // base | min | web | profile | flavor
	Title      string            `json:"title"`  // что добавляет ступень
	Source     string            `json:"source"` // откуда Dockerfile
	Dockerfile string            `json:"dockerfile"`
	Args       map[string]string `json:"args,omitempty"`    // build-arg'и сборки
	Context    []ContextItem     `json:"context,omitempty"` // что ещё в контексте сборки
	From       []string          `json:"from,omitempty"`    // базовые образы
	Apt        []string          `json:"apt,omitempty"`     // пакеты apt
	Npm        []string          `json:"npm,omitempty"`     // пакеты npm (и прогрев npx)
	Downloads  []string          `json:"downloads,omitempty"`
}

// Откуда файл контекста сборки (ContextItem.Origin).
const (
	CtxEmbedded = "embedded" // встроен в бинарь agctl (internal/assets/image)
	CtxSkel     = "skel"     // скелет dsh-home, встроен в бинарь agctl (internal/assets/dsh-skel)
	CtxGuest    = "guest"    // guest-бинарь linux на диске (<корень>/bin/guest)
	CtxProfile  = "profile"  // файл профиля организации
)

// ContextItem — файл или каталог контекста сборки ступени, кроме Dockerfile:
// откуда он берётся, размер, у каталога — число файлов.
type ContextItem struct {
	Name   string `json:"name"` // путь в контексте: entrypoint.sh, skel/, agctl
	Dir    bool   `json:"dir,omitempty"`
	Origin string `json:"origin"` // CtxEmbedded | CtxSkel | CtxGuest | CtxProfile
	From   string `json:"from"`   // откуда — для человека
	// Path — где именно: путь в исходниках agctl (встроенные), внутри
	// профиля или на диске (guest-бинарь).
	Path     string `json:"path,omitempty"`
	Size     int64  `json:"size"`               // байт; у каталога — всех файлов
	Files    int    `json:"files,omitempty"`    // число файлов каталога
	Missing  bool   `json:"missing,omitempty"`  // guest-бинаря нет на диске
	Modified string `json:"modified,omitempty"` // дата guest-бинаря на диске
	Note     string `json:"note,omitempty"`     // что сделать, если файла нет
}

// stageSpec — откуда берётся ступень: Dockerfile и контекст сборки.
type stageSpec struct {
	st     DevStage
	fsys   fs.FS  // контекст: встроенный image/, вкус, надстройка профиля
	dir    string // корень контекста в fsys
	file   string // Dockerfile от корня контекста
	origin string // CtxEmbedded | CtxProfile — чьи файлы в fsys
	where  string // корень контекста в исходниках agctl или в профиле
	base   bool   // base/min: в контексте ещё skel/ (SkelFS) и agctl (guest)
}

// devStageSpecs — ступени DevBuildPlan(b) с источниками Dockerfile и контекста.
func devStageSpecs(b DevBuild) ([]stageSpec, error) {
	plan := DevBuildPlan(b)
	tag := imageTag(b.Target)
	base := "agctl-dsh:" + tag
	labels := map[string]string{}
	for k, v := range map[string]string{"AGCTL_VERSION": b.Version, "AGCTL_COMMIT": b.Commit, "AGCTL_BUILD_DATE": b.BuildDate} {
		if v != "" {
			labels[k] = v
		}
	}
	dsh := assets.DshVersionFromTag(tag)
	dshArg := dsh
	if dshArg == "" {
		dshArg = assets.PinnedDshVersion() + " (пин сборки agctl)"
	}
	overlayDir := strings.Trim(path.Clean("/"+b.OverlayDir), "/")
	if overlayDir == "" {
		overlayDir = "."
	}
	var out []stageSpec
	prev := ""
	for _, ref := range plan {
		sp := stageSpec{st: DevStage{Ref: ref, Args: map[string]string{}}, file: "Dockerfile"}
		switch {
		case ref == base:
			sp.st.Kind, sp.st.Title = "base", "базовый: Debian и Node, инструменты (python, git, LibreOffice, glab, jira-cli, uv, kubectl), прогретые MCP, dsh, guest agctl"
			sp.fsys, sp.dir, sp.st.Source = b.ImageFS, "image", "встроен в agctl: image/Dockerfile"
			sp.origin, sp.base = CtxEmbedded, true
			sp.st.Args["DSH_VERSION"] = dshArg
		case ref == "agctl-dsh-min:"+tag:
			sp.st.Kind, sp.st.Title = "min", "минимальный: Debian и Node, dsh и guest agctl — без офиса, CLI и прогретых MCP"
			sp.fsys, sp.dir, sp.file, sp.st.Source = b.ImageFS, "image", "Dockerfile.min", "встроен в agctl: image/Dockerfile.min"
			sp.origin, sp.base = CtxEmbedded, true
			sp.st.Args["DSH_VERSION"] = dshArg
		case ref == "agctl-dsh-web:"+tag:
			sp.st.Kind, sp.st.Title = "web", "вкус web: Chromium и Playwright поверх базового"
			sp.fsys, sp.dir, sp.st.Source = b.ImageFS, "image/flavors/web", "встроен в agctl: image/flavors/web/Dockerfile"
			sp.origin = CtxEmbedded
			sp.st.Args["BASE"] = prev
		case b.Overlay != nil && ref == withSuffix(prev, b.ProfileID):
			sp.st.Kind, sp.st.Title = "profile", "надстройка профиля "+b.ProfileID
			sp.fsys, sp.dir, sp.st.Source = b.Overlay, ".", "профиль "+b.ProfileID+": images.dockerfile"
			if b.OverlayDockerfile != "" {
				sp.file = b.OverlayDockerfile
			}
			sp.origin, sp.where = CtxProfile, overlayDir
			sp.st.Args["BASE"] = prev
		default:
			sp.st.Kind, sp.st.Title = "flavor", "вкус профиля "+b.Flavor
			sp.fsys, sp.dir = b.ProfileFS, path.Join("image", "flavors", b.Flavor)
			sp.st.Source = "профиль " + b.ProfileID + ": " + path.Join(sp.dir, "Dockerfile")
			sp.origin, sp.where = CtxProfile, sp.dir
			sp.st.Args["BASE"] = prev
		}
		if sp.origin == CtxEmbedded {
			sp.where = path.Join("internal/assets", sp.dir)
		}
		if sp.fsys == nil {
			return nil, fmt.Errorf("ступень %s: нет контекста сборки", ref)
		}
		if sp.base {
			for k, v := range labels {
				sp.st.Args[k] = v
			}
		}
		out = append(out, sp)
		prev = ref
	}
	return out, nil
}

// DevBuildStages — ступени DevBuildPlan(b) с содержимым. Ошибка — если
// Dockerfile ступени не найден (встроенный контекст или профиль).
func DevBuildStages(b DevBuild) ([]DevStage, error) {
	specs, err := devStageSpecs(b)
	if err != nil {
		return nil, err
	}
	out := make([]DevStage, 0, len(specs))
	for _, sp := range specs {
		st := sp.st
		file := path.Join(sp.dir, sp.file)
		raw, err := fs.ReadFile(sp.fsys, file)
		if err != nil {
			return nil, fmt.Errorf("ступень %s: %s: %w", st.Ref, file, err)
		}
		st.Dockerfile = string(raw)
		st.Context = sp.contextItems(b)
		analyzeDockerfile(&st)
		out = append(out, st)
	}
	return out, nil
}

// contextItems — файлы контекста сборки рядом с Dockerfile (кроме самих
// Dockerfile и вложенных вкусов), у базового и минимального — ещё скелет
// dsh-home и guest-бинарь.
func (sp stageSpec) contextItems(b DevBuild) []ContextItem {
	var out []ContextItem
	from := "встроен в бинарь agctl"
	if sp.origin == CtxProfile {
		from = "файл профиля " + b.ProfileID
	}
	ents, _ := fs.ReadDir(sp.fsys, sp.dir)
	for _, e := range ents {
		n := e.Name()
		if n == sp.file || strings.HasPrefix(n, "Dockerfile") || n == "flavors" || strings.HasPrefix(n, ".") {
			continue
		}
		it := ContextItem{Name: n, Origin: sp.origin, From: from, Path: path.Join(sp.where, n)}
		if e.IsDir() {
			it.Name += "/"
			it.Path += "/"
			it.Dir = true
			it.Files, it.Size = treeSize(sp.fsys, path.Join(sp.dir, n))
		} else if info, err := e.Info(); err == nil {
			it.Size = info.Size()
		}
		out = append(out, it)
	}
	if !sp.base {
		return out
	}
	skel := ContextItem{Name: "skel/", Dir: true, Origin: CtxSkel, From: "скелет dsh-home, встроен в бинарь agctl", Path: "internal/assets/dsh-skel/"}
	if b.SkelFS != nil {
		skel.Files, skel.Size = treeSize(b.SkelFS, "dsh-skel")
	}
	return append(out, skel, guestItem(b))
}

// guestItem — guest-бинарь linux для COPY agctl: его ищет findLinuxBinary
// в BinDir (<корень>/bin/guest — туда его кладёт install.sh) и рядом с
// бинарём agctl. Версию не спрашиваем: linux-бинарь на macOS не запустить.
func guestItem(b DevBuild) ContextItem {
	it := ContextItem{Name: "agctl", Origin: CtxGuest, From: "guest-бинарь на диске"}
	arch := b.planArch()
	p, err := findLinuxBinary(b.BinDir, arch)
	if err != nil {
		name := "agctl-guest-linux-" + arch
		if arch == "" {
			name = "agctl-guest-linux-<arm64|amd64>"
		}
		it.Missing, it.Note = true, err.Error()
		it.Path = filepath.Join(b.BinDir, name)
		return it
	}
	it.Path = p
	if st, err := os.Stat(p); err == nil {
		it.Size = st.Size()
		it.Modified = st.ModTime().Format("2006-01-02 15:04")
	}
	return it
}

// planArch — архитектура guest-бинаря для плана: демона Docker (Arch), если
// её узнали, иначе этой машины.
func (b DevBuild) planArch() string {
	if b.Arch != "" {
		return b.Arch
	}
	return goArch(runtime.GOARCH)
}

// treeSize — число файлов и их общий размер в каталоге dir.
func treeSize(fsys fs.FS, dir string) (files int, size int64) {
	_ = fs.WalkDir(fsys, dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			files++
			size += info.Size()
		}
		return nil
	})
	return files, size
}

// MaxContextText — предел текста файла контекста для просмотра.
const MaxContextText = 256 << 10

// ContextFile — файл контекста сборки (текст) или каталог (список файлов).
type ContextFile struct {
	Path  string         `json:"path"`
	Size  int64          `json:"size"`
	Dir   bool           `json:"dir,omitempty"`
	Files []ContextEntry `json:"files,omitempty"` // каталог: все файлы, пути от корня контекста
	Text  string         `json:"text,omitempty"`
}

// ContextEntry — файл каталога контекста.
type ContextEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DevContextFile — файл или каталог rel контекста сборки ступени stage (с 1,
// как в плане) образа b.Target: текст файла (до MaxContextText, только
// текст) или список файлов каталога. Двоичный или больший файл — ошибка с
// размером; нет такого пути или ступени — ошибка с fs.ErrNotExist.
func DevContextFile(b DevBuild, stage int, rel string) (ContextFile, error) {
	rel = strings.TrimSuffix(strings.TrimSpace(rel), "/")
	if rel == "" || rel == "." || !fs.ValidPath(rel) {
		return ContextFile{}, fmt.Errorf("путь %q: нужен путь внутри контекста сборки, без «..»", rel)
	}
	specs, err := devStageSpecs(b)
	if err != nil {
		return ContextFile{}, err
	}
	if stage < 1 || stage > len(specs) {
		return ContextFile{}, notExist("у образа %s ступеней %d — ступени %d нет", b.Target, len(specs), stage)
	}
	sp := specs[stage-1]
	switch {
	case sp.base && rel == "agctl":
		it := guestItem(b)
		if it.Missing {
			return ContextFile{}, notExist("%s", it.Note)
		}
		f, err := os.Open(it.Path)
		if err != nil {
			return ContextFile{}, err
		}
		defer f.Close()
		text, err := contextText(rel, it.Size, f)
		return ContextFile{Path: rel, Size: it.Size, Text: text}, err
	case sp.base && (rel == "skel" || strings.HasPrefix(rel, "skel/")):
		if b.SkelFS == nil {
			return ContextFile{}, notExist("%s: в этой сборке нет скелета dsh-home", rel)
		}
		return readContextPath(b.SkelFS, path.Join("dsh-skel", strings.TrimPrefix(strings.TrimPrefix(rel, "skel"), "/")), rel)
	}
	return readContextPath(sp.fsys, path.Join(sp.dir, rel), rel)
}

// notExistError — «нет такого пути или ступени» по-русски; errors.Is(err,
// fs.ErrNotExist) — истина (лаунчер отвечает 404).
type notExistError struct{ msg string }

func (e notExistError) Error() string { return e.msg }
func (e notExistError) Unwrap() error { return fs.ErrNotExist }

func notExist(format string, args ...any) error {
	return notExistError{msg: fmt.Sprintf(format, args...)}
}

// readContextPath — name из fsys под путём rel контекста: каталог — список
// файлов, файл — текст.
func readContextPath(fsys fs.FS, name, rel string) (ContextFile, error) {
	st, err := fs.Stat(fsys, name)
	if err != nil {
		return ContextFile{}, notExist("%s: нет в контексте сборки", rel)
	}
	if st.IsDir() {
		out := ContextFile{Path: rel, Dir: true}
		err := fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out.Files = append(out.Files, ContextEntry{Path: rel + strings.TrimPrefix(p, name), Size: info.Size()})
			out.Size += info.Size()
			return nil
		})
		return out, err
	}
	f, err := fsys.Open(name)
	if err != nil {
		return ContextFile{}, err
	}
	defer f.Close()
	text, err := contextText(rel, st.Size(), f)
	return ContextFile{Path: rel, Size: st.Size(), Text: text}, err
}

// contextText — содержимое файла как текст: двоичный (NUL или не UTF-8 в
// начале) или больше MaxContextText — ошибка с размером.
func contextText(rel string, size int64, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxContextText+1))
	if err != nil {
		return "", err
	}
	head := data
	if len(head) > 8<<10 {
		head = trimPartialRune(head[:8<<10])
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
		return "", fmt.Errorf("%s — двоичный файл, %s: показываются только текстовые", rel, humanSize(size))
	}
	if size > MaxContextText || len(data) > MaxContextText {
		return "", fmt.Errorf("%s — %s: показываются файлы до %s", rel, humanSize(size), humanSize(MaxContextText))
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s — двоичный файл, %s: показываются только текстовые", rel, humanSize(size))
	}
	return string(data), nil
}

// trimPartialRune отрезает недочитанную последнюю руну UTF-8.
func trimPartialRune(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

// humanSize — размер для человека: 980 Б, 12.3 КБ, 41.2 МБ, 4.4 ГБ.
func humanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d Б", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f КБ", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f ГБ", float64(n)/(1<<30))
}

var (
	argRe    = regexp.MustCompile(`^ARG\s+([A-Za-z_][A-Za-z0-9_]*)(?:=(\S*))?`)
	fromRe   = regexp.MustCompile(`(?i)^FROM\s+(\S+)(?:\s+AS\s+(\S+))?`)
	varRe    = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	urlRe    = regexp.MustCompile(`https?://[^\s"'|)]+`)
	aptRe    = regexp.MustCompile(`apt-get install\s+([^&;|]+)`)
	npmRe    = regexp.MustCompile(`npm (?:install|i) -g\s+([^&;|]+)`)
	forPkgRe = regexp.MustCompile(`for pkg in\s+([^;]+);`)
)

// analyzeDockerfile — выжимка Dockerfile ступени: базовые образы, пакеты apt и
// npm, загрузки по сети (с подстановкой ARG по умолчанию и аргументов
// сборки). Эвристика для интроспекции, а не разбор Dockerfile целиком.
func analyzeDockerfile(st *DevStage) {
	vars := map[string]string{}
	stages := map[string]bool{}
	sub := func(s string) string {
		return varRe.ReplaceAllStringFunc(s, func(m string) string {
			name := varRe.FindStringSubmatch(m)[1]
			if v, ok := st.Args[name]; ok && !strings.Contains(v, " ") {
				return v
			}
			if v, ok := vars[name]; ok && v != "" {
				return v
			}
			return m
		})
	}
	apt, npm, dl := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, line := range dockerfileLines(st.Dockerfile) {
		if m := argRe.FindStringSubmatch(line); m != nil {
			if _, set := vars[m[1]]; !set {
				vars[m[1]] = m[2]
			}
			continue
		}
		if m := fromRe.FindStringSubmatch(line); m != nil {
			img := sub(m[1])
			if !stages[img] {
				st.From = append(st.From, img)
			}
			if m[2] != "" {
				stages[m[2]] = true
			}
			continue
		}
		for _, m := range aptRe.FindAllStringSubmatch(line, -1) {
			for _, w := range strings.Fields(m[1]) {
				if !strings.HasPrefix(w, "-") {
					apt[w] = true
				}
			}
		}
		for _, m := range npmRe.FindAllStringSubmatch(line, -1) {
			for _, w := range strings.Fields(m[1]) {
				if !strings.HasPrefix(w, "-") {
					npm[sub(w)] = true
				}
			}
		}
		for _, m := range forPkgRe.FindAllStringSubmatch(line, -1) {
			for _, w := range strings.Fields(m[1]) {
				npm[sub(w)+" (прогрев npx)"] = true
			}
		}
		// Загрузки — только из RUN и ADD (адреса в LABEL — описание, не сеть).
		if up := strings.ToUpper(line); strings.HasPrefix(up, "RUN ") || strings.HasPrefix(up, "ADD ") {
			for _, u := range urlRe.FindAllString(line, -1) {
				dl[sub(strings.TrimRight(u, `\`))] = true
			}
			if strings.Contains(line, "playwright") && strings.Contains(line, " install") {
				dl["браузер Chromium и его системные пакеты (playwright install --with-deps)"] = true
			}
		}
	}
	st.Apt, st.Npm, st.Downloads = sortedKeys(apt), sortedKeys(npm), sortedKeys(dl)
}

// dockerfileLines — инструкции Dockerfile: продолжения строк склеены,
// комментарии и пустые строки убраны.
func dockerfileLines(text string) []string {
	var out []string
	var cur strings.Builder
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") || (line == "" && cur.Len() == 0) {
			continue
		}
		if strings.HasSuffix(line, `\`) {
			cur.WriteString(strings.TrimSuffix(line, `\`))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(line)
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
