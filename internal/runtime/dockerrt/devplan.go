package dockerrt

// devplan.go — что войдёт в образ локального режима, если собирать его на
// этой машине: ступени цепочки DevBuildPlan, у каждой — Dockerfile (текст),
// build-arg'и, что ещё лежит в контексте сборки и что качается из сети
// (базовые образы, пакеты apt и npm, загрузки). Для лаунчера («Образы» →
// «Как собирается») и интроспекции; собирает BuildDevImage — тем же порядком
// ступеней и с теми же аргументами.

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

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
	Context    []string          `json:"context,omitempty"` // что ещё в контексте сборки
	From       []string          `json:"from,omitempty"`    // базовые образы
	Apt        []string          `json:"apt,omitempty"`     // пакеты apt
	Npm        []string          `json:"npm,omitempty"`     // пакеты npm (и прогрев npx)
	Downloads  []string          `json:"downloads,omitempty"`
}

// DevBuildStages — ступени DevBuildPlan(b) с содержимым. Ошибка — если
// Dockerfile ступени не найден (встроенный контекст или профиль).
func DevBuildStages(b DevBuild) ([]DevStage, error) {
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
	var out []DevStage
	prev := ""
	for _, ref := range plan {
		st := DevStage{Ref: ref, Args: map[string]string{}}
		var fsys fs.FS
		var file string
		switch {
		case ref == base:
			st.Kind, st.Title = "base", "базовый: Debian и Node, инструменты (python, git, LibreOffice, glab, jira-cli, uv, kubectl), прогретые MCP, dsh, guest agctl"
			fsys, file, st.Source = b.ImageFS, "image/Dockerfile", "встроен в agctl: image/Dockerfile"
			st.Context = append(devContextFiles(b.ImageFS, "image"), "skel/ — скелет dsh-home: плагины и настройки agctl", "agctl — guest-бинарь linux этой сборки agctl")
			st.Args["DSH_VERSION"] = dshArg
		case ref == "agctl-dsh-min:"+tag:
			st.Kind, st.Title = "min", "минимальный: Debian и Node, dsh и guest agctl — без офиса, CLI и прогретых MCP"
			fsys, file, st.Source = b.ImageFS, "image/Dockerfile.min", "встроен в agctl: image/Dockerfile.min"
			st.Context = append(devContextFiles(b.ImageFS, "image"), "skel/ — скелет dsh-home: плагины и настройки agctl", "agctl — guest-бинарь linux этой сборки agctl")
			st.Args["DSH_VERSION"] = dshArg
		case ref == "agctl-dsh-web:"+tag:
			st.Kind, st.Title = "web", "вкус web: Chromium и Playwright поверх базового"
			fsys, file, st.Source = b.ImageFS, "image/flavors/web/Dockerfile", "встроен в agctl: image/flavors/web/Dockerfile"
			st.Args["BASE"] = prev
		case b.Overlay != nil && ref == withSuffix(prev, b.ProfileID):
			st.Kind, st.Title = "profile", "надстройка профиля "+b.ProfileID
			fsys, file, st.Source = b.Overlay, b.OverlayDockerfile, "профиль "+b.ProfileID+": images.dockerfile"
			if file == "" {
				file = "Dockerfile"
			}
			st.Context = devContextFiles(b.Overlay, ".")
			st.Args["BASE"] = prev
		default:
			st.Kind, st.Title = "flavor", "вкус профиля "+b.Flavor
			fsys, file = b.ProfileFS, path.Join("image", "flavors", b.Flavor, "Dockerfile")
			st.Source = "профиль " + b.ProfileID + ": " + file
			st.Args["BASE"] = prev
		}
		if fsys == nil {
			return nil, fmt.Errorf("ступень %s: нет контекста сборки", ref)
		}
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, fmt.Errorf("ступень %s: %s: %w", ref, file, err)
		}
		if st.Kind == "base" || st.Kind == "min" {
			for k, v := range labels {
				st.Args[k] = v
			}
		}
		st.Dockerfile = string(raw)
		analyzeDockerfile(&st)
		out = append(out, st)
		prev = ref
	}
	return out, nil
}

// devContextFiles — файлы контекста сборки рядом с Dockerfile (кроме самих
// Dockerfile и вложенных вкусов).
func devContextFiles(fsys fs.FS, dir string) []string {
	if fsys == nil {
		return nil
	}
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "Dockerfile") || n == "flavors" || strings.HasPrefix(n, ".") {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		out = append(out, n)
	}
	return out
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
