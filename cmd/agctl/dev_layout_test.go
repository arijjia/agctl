package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/server"
)

// layoutRoot — изолированный корень и файловое хранилище секретов: тесты не
// трогают ни ~/.agctl, ни хранилище ОС.
func layoutRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	t.Setenv("AGCTL_ROOT", root)
	t.Setenv("AGCTL_DEV_HOME", "")
	t.Setenv("AGCTL_HOME_ID", "")
	t.Setenv("AGCTL_DEV_SECRETS", "file")
	return root
}

func writeOrgProfile(t *testing.T, dir, id, extra string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema: 1\nid: " + id + "\nbrand:\n  name: " + id + "\nimages:\n  dev:\n    - agctl-dsh-" + id + ":0.2.2\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := "tools:\n  - name: org_tool\n    description: инструмент организации\n    command: [echo, org]\n"
	if err := os.WriteFile(filepath.Join(dir, "local", "bridge-tools.yaml"), []byte(tools), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDevSplitHomeFlag(t *testing.T) {
	for _, c := range []struct {
		in   []string
		id   string
		rest []string
	}{
		{[]string{"up"}, "", []string{"up"}},
		{[]string{"--home", "utmn", "up", "--no-open"}, "utmn", []string{"up", "--no-open"}},
		{[]string{"up", "--home=utmn", "/src"}, "utmn", []string{"up", "/src"}},
		{[]string{"paths", "--json", "--home", "rshb"}, "rshb", []string{"paths", "--json"}},
	} {
		id, rest, err := devSplitHomeFlag(c.in)
		if err != nil || id != c.id || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%v → %q %v %v", c.in, id, rest, err)
		}
	}
	if _, _, err := devSplitHomeFlag([]string{"up", "--home"}); err == nil {
		t.Error("--home без значения принят")
	}
}

// dev serve снимает серверное окружение (.env make): AGCTL_LLM_*,
// AGCTL_EGRESS_*, публичный URL, CORS — и не трогает выбор корня и дома.
func TestDevForeignEnv(t *testing.T) {
	got := devForeignEnv([]string{
		"PATH=/bin", "AGCTL_ROOT=/r", "AGCTL_DEV_HOME=/h", "AGCTL_HOME_ID=utmn", "AGCTL_DEV_SECRETS=file",
		"AGCTL_LLM_API_KEY=k", "AGCTL_LLM_BASE_URL=u", "AGCTL_LLM_MODEL=m", "AGCTL_LLM_MODELS=a,b",
		"AGCTL_LLM_PROVIDER=p", "AGCTL_LLM_EXTRA_PROVIDERS=[]", "AGCTL_PUBLIC_URL=x", "AGCTL_CORS_ORIGINS=y",
		"AGCTL_EGRESS_ALLOWLIST=1", "AGCTL_EGRESS_ALLOW=h", "AGCTL_PROFILE=utmn", "AGCTL_PROFILE_DIR=/p",
		"AGCTL_ADDR=:1", "AGCTL_DEV_ENV=A", "AGCTL_DEV_IMAGES=i", "AGCTL_HOME=/srv", "AGCTL_CONFIG=/c",
	})
	want := []string{
		"AGCTL_ADDR", "AGCTL_CONFIG", "AGCTL_CORS_ORIGINS", "AGCTL_DEV_ENV", "AGCTL_DEV_IMAGES",
		"AGCTL_EGRESS_ALLOW", "AGCTL_EGRESS_ALLOWLIST", "AGCTL_HOME",
		"AGCTL_LLM_API_KEY", "AGCTL_LLM_BASE_URL", "AGCTL_LLM_EXTRA_PROVIDERS", "AGCTL_LLM_MODEL",
		"AGCTL_LLM_MODELS", "AGCTL_LLM_PROVIDER", "AGCTL_PROFILE", "AGCTL_PROFILE_DIR", "AGCTL_PUBLIC_URL",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("снимается:\n%v\nждём:\n%v", got, want)
	}
}

func TestDevInitHomeFollowsProfile(t *testing.T) {
	layoutRoot(t)
	def := localmode.Home{ID: "agctl", Dir: localmode.HomeDir("agctl"), Source: localmode.HomeFromDefault}
	if h := devInitHome(def, "utmn", ""); h.ID != "utmn" || h.Dir != localmode.HomeDir("utmn") {
		t.Fatalf("init --profile utmn на чистой машине — дом utmn: %+v", h)
	}
	if h := devInitHome(def, "", ""); h.ID != "agctl" {
		t.Fatalf("без профиля — дом по умолчанию: %+v", h)
	}
	flag := localmode.Home{ID: "work", Dir: localmode.HomeDir("work"), Source: localmode.HomeFromFlag}
	if h := devInitHome(flag, "utmn", ""); h.ID != "work" {
		t.Fatalf("явный --home не переопределяется: %+v", h)
	}
	if err := (&localmode.Config{}).Save(def.Dir); err != nil {
		t.Fatal(err)
	}
	if h := devInitHome(def, "utmn", ""); h.ID != "agctl" {
		t.Fatalf("настроенный дом по умолчанию не подменяется: %+v", h)
	}
}

// init не копирует из профиля ни образ, ни инструменты моста.
func TestDevInitCopiesNothingFromProfile(t *testing.T) {
	layoutRoot(t)
	writeOrgProfile(t, filepath.Join(localmode.ProfilesDir(), "utmn"), "utmn", "")
	work := t.TempDir()
	h, err := localmode.SelectHome("")
	if err != nil {
		t.Fatal(err)
	}
	if err := devInit(h, []string{"--yes", "--profile", "utmn", "--workdir", work}); err != nil {
		t.Fatal(err)
	}
	home := localmode.HomeDir("utmn")
	cfg, err := localmode.Load(home)
	if err != nil {
		t.Fatalf("дом homes/utmn не создан: %v", err)
	}
	if cfg.Image != "" || len(cfg.Bridge.Tools) != 0 || cfg.Profile != "utmn" || cfg.ProfileDir != "" {
		t.Fatalf("в config.yaml попало лишнее: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "dev.yaml")); !os.IsNotExist(err) {
		t.Fatal("dev.yaml создаваться не должен")
	}
}

func TestDevPathsReport(t *testing.T) {
	root := layoutRoot(t)
	repo := filepath.Join(t.TempDir(), "agctl-profile-utmn")
	writeOrgProfile(t, repo, "utmn", "")
	if err := os.MkdirAll(localmode.ProfilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, filepath.Join(localmode.ProfilesDir(), "utmn")); err != nil {
		t.Skipf("симлинки недоступны: %v", err)
	}
	work := t.TempDir()
	cfg := &localmode.Config{Profile: "utmn", WorkDir: work, Addr: "127.0.0.1:8931", Secrets: []string{"GITLAB_TOKEN"}}
	home := localmode.HomeDir("utmn")
	if err := cfg.Save(home); err != nil {
		t.Fatal(err)
	}
	h, _ := localmode.SelectHome("")
	rep := devPathsReport(h)
	data := filepath.Join(home, "data")
	if rep.Root.Path != root || rep.Home.ID != "utmn" || rep.Home.Dir != home || !rep.Home.Initialized || rep.Home.Source != localmode.HomeFromSingle {
		t.Fatalf("дом: %+v / %+v", rep.Root, rep.Home)
	}
	if rep.Profile.ID != "utmn" || rep.Profile.Origin != localmode.ProfileFromRoot || rep.Profile.Link == "" {
		t.Fatalf("профиль: %+v", rep.Profile)
	}
	if rep.Image.Ref != "agctl-dsh-utmn:0.2.2" || !strings.Contains(rep.Image.Source, "профиль utmn") {
		t.Fatalf("образ по профилю: %+v", rep.Image)
	}
	if rep.Bridge.Port != "8931" || rep.Bridge.Tools != 1 || rep.UI.URL != "http://127.0.0.1:8931" || rep.UI.Running {
		t.Fatalf("сеть: %+v %+v", rep.UI, rep.Bridge)
	}
	if rep.Docker.Containers[0].Name != server.ChatContainerName(data, 1, 0) || !strings.HasPrefix(rep.Docker.EgressNetwork, "agctl-egress-") {
		t.Fatalf("docker: %+v", rep.Docker)
	}
	if rep.Docker.Mounts[0].Host != work || rep.Docker.Mounts[0].Container != "/workspace" ||
		rep.Docker.Mounts[1].Host != filepath.Join(data, "chats", "u1", "dsh-home") || rep.Docker.Mounts[1].Container != "/home/dsh" {
		t.Fatalf("маунты: %+v", rep.Docker.Mounts)
	}
	if !reflect.DeepEqual(rep.Secrets.Names, []string{"GITLAB_TOKEN"}) || rep.Secrets.File == "" {
		t.Fatalf("секреты: %+v", rep.Secrets)
	}
	byName := map[string]pathItem{}
	for _, f := range rep.Home.Files {
		if f.Purpose == "" || !strings.HasPrefix(f.Path, home) {
			t.Errorf("файл дома без назначения или вне дома: %+v", f)
		}
		byName[f.Name] = f
	}
	for _, name := range []string{"config.yaml", "data/", "data/agctl.db", "logs/serve.log", "logs/bridge.log", "run/state.json", "backups/", "secrets.enc"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("в отчёте нет %s", name)
		}
	}
	if !byName["config.yaml"].Exists || byName["run/state.json"].Exists {
		t.Fatalf("наличие файлов: %+v", byName)
	}

	var text bytes.Buffer
	devPathsPrint(&text, rep)
	// Цель симлинка отчёт печатает через EvalSymlinks: на Windows короткое
	// имя каталога (RUNNER~1) раскрывается в длинное, на macOS /var — в
	// /private/var. Сверяем с той же канонической формой.
	repoShown := repo
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repoShown = r
	}
	for _, want := range []string{"КОРЕНЬ", "БИНАРИ", "GUEST", "ПРОФИЛЬ", "ДОМ", "ПАПКА ПРОЕКТОВ", "СЕТЬ", "ОБРАЗ", "DOCKER", "СЕКРЕТЫ", root, repoShown, work, "/workspace", "/home/dsh", "GITLAB_TOKEN"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("в выводе нет %q", want)
		}
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"root", "binaries", "guest", "profile", "home", "workdir", "ui", "bridge", "image", "docker", "secrets"} {
		if _, ok := back[key]; !ok {
			t.Errorf("в JSON нет %q", key)
		}
	}

	// ненастроенный дом — отчёт всё равно строится
	fresh := devPathsReport(localmode.Home{ID: "new", Dir: localmode.HomeDir("new"), Source: localmode.HomeFromFlag})
	if fresh.Home.Initialized || fresh.Profile.ID != "agctl" || !reflect.DeepEqual(fresh.Home.Others, []string{"utmn"}) {
		t.Fatalf("ненастроенный дом: %+v %+v", fresh.Home, fresh.Profile)
	}
}

func TestDevMigrate(t *testing.T) {
	layoutRoot(t)
	t.Setenv("AGCTL_DEV_SECRETS", "")
	keyring.MockInit()
	base := t.TempDir()
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	repo := filepath.Join(base, "src", "agctl-profile-utmn")
	writeOrgProfile(t, repo, "utmn", "")
	mk := func(name, devYAML string) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
			t.Fatal(err)
		}
		if devYAML != "" {
			if err := os.WriteFile(filepath.Join(dir, "dev.yaml"), []byte(devYAML), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_ = os.WriteFile(filepath.Join(dir, "data", "agctl.db"), []byte("db"), 0o600)
		return dir
	}
	// нейтральный дом; дом организации с явным profile_dir, скопированными из
	// профиля образом и инструментом и одним личным инструментом; второй дом
	// того же профиля (конфликт); каталог без конфига.
	neutral := mk(".agctl-dev", "profile: agctl\nimage: agctl-dsh:0.2.2\naddr: 127.0.0.1:8930\n")
	utmn := mk(".agctl-dev-utmn", "profile_dir: "+repo+"\nimage: agctl-dsh-utmn:0.2.2\naddr: 127.0.0.1:8931\nsecrets: [GITLAB_TOKEN]\nbridge:\n  tools:\n    - name: org_tool\n      description: инструмент организации\n      command: [echo, org]\n    - name: mine\n      command: [echo, mine]\n")
	dup := mk(".agctl-dev-utmn2", "profile_dir: "+repo+"\n")
	empty := mk(".agctl-dev-junk", "")
	if err := keyring.Set(localmode.LegacyKeyringService, "GITLAB_TOKEN", "legacy-token"); err != nil {
		t.Fatal(err)
	}

	items, err := migratePlan(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("план: %+v", items)
	}
	by := map[string]migrateItem{}
	for _, it := range items {
		by[it.From] = it
	}
	if it := by[neutral]; it.ID != "agctl" || it.To != localmode.HomeDir("agctl") || it.Skip != "" {
		t.Fatalf("нейтральный: %+v", it)
	}
	if it := by[utmn]; it.ID != "utmn" || it.Skip != "" || it.ProfileLink != filepath.Join(localmode.ProfilesDir(), "utmn") {
		t.Fatalf("utmn: %+v", it)
	}
	if it := by[dup]; !strings.Contains(it.Skip, "уже занят") {
		t.Fatalf("конфликт id не замечен: %+v", it)
	}
	if it := by[empty]; !strings.Contains(it.Skip, "нет dev.yaml") {
		t.Fatalf("каталог без конфига: %+v", it)
	}

	notRunning := func(string) bool { return false }
	// dry-run: на диске ничего не меняется, предупреждение про контейнеры есть
	var plan bytes.Buffer
	if err := migrateRun(&plan, items, false, notRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(localmode.HomesDir()); !os.IsNotExist(err) {
		t.Fatal("dry-run создал homes/")
	}
	if _, err := os.Stat(filepath.Join(utmn, "dev.yaml")); err != nil {
		t.Fatal("dry-run тронул прежний дом")
	}
	oldPrefix := server.ChatContainerPrefix(filepath.Join(utmn, "data"))
	newPrefix := server.ChatContainerPrefix(filepath.Join(localmode.HomeDir("utmn"), "data"))
	if oldPrefix == newPrefix {
		t.Fatal("имена контейнеров обязаны различаться")
	}
	for _, want := range []string{"dry-run", oldPrefix, newPrefix, "docker rm -f", "--apply"} {
		if !strings.Contains(plan.String(), want) {
			t.Errorf("в плане нет %q:\n%s", want, plan.String())
		}
	}
	// запущенный дом не переносится
	var busy bytes.Buffer
	if err := migrateRun(&busy, append([]migrateItem{}, items...), true, func(h string) bool { return h == neutral }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(neutral); err != nil || !strings.Contains(busy.String(), "запущена") {
		t.Fatalf("запущенный дом тронут: %v\n%s", err, busy.String())
	}

	// apply (utmn уже перенесён предыдущим вызовом; нейтральный — сейчас)
	items, _ = migratePlan(base)
	var out bytes.Buffer
	if err := migrateRun(&out, items, true, notRunning); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, gone := range []string{neutral, utmn} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s должен переехать", gone)
		}
	}
	if _, err := os.Stat(dup); err != nil {
		t.Error("конфликтный дом должен остаться на месте")
	}
	home := localmode.HomeDir("utmn")
	cfg, err := localmode.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "utmn" || cfg.ProfileDir != "" || cfg.Image != "" || cfg.Addr != "127.0.0.1:8931" {
		t.Fatalf("конфиг после переноса: %+v", cfg)
	}
	if len(cfg.Bridge.Tools) != 1 || cfg.Bridge.Tools[0].Name != "mine" {
		t.Fatalf("личные инструменты: %+v", cfg.Bridge.Tools)
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "dev.yaml")); !os.IsNotExist(err) {
		t.Fatal("dev.yaml остался рядом с config.yaml")
	}
	if raw, _ := os.ReadFile(filepath.Join(home, "data", "agctl.db")); string(raw) != "db" {
		t.Fatal("данные дома не переехали")
	}
	if target, err := filepath.EvalSymlinks(filepath.Join(localmode.ProfilesDir(), "utmn")); err != nil || target != repo {
		t.Fatalf("ссылка на профиль: %q %v", target, err)
	}
	if p, ref, err := localmode.ResolveProfile(cfg); err != nil || p.ID != "utmn" || ref.Origin != localmode.ProfileFromRoot {
		t.Fatalf("профиль после переноса: %+v %v", ref, err)
	}
	if v, _ := keyring.Get(localmode.KeyringService("utmn"), "GITLAB_TOKEN"); v != "legacy-token" {
		t.Fatal("секрет не скопирован в сервис дома")
	}
	if v, _ := keyring.Get(localmode.LegacyKeyringService, "GITLAB_TOKEN"); v != "legacy-token" {
		t.Fatal("легаси-запись должна остаться")
	}
	if n, err := localmode.Load(localmode.HomeDir("agctl")); err != nil || n.Image != "" {
		t.Fatalf("нейтральный дом: %+v %v", n, err)
	}
}

// Первый `agctl dev up` без единого дома настраивает дом по умолчанию сам:
// папка проектов — аргумент up (создаётся), с путями как на хосте; при
// существующем доме ничего не трогает.
func TestDevAutoInit(t *testing.T) {
	layoutRoot(t)
	h, _ := localmode.SelectHome("")
	work := filepath.Join(t.TempDir(), "src")
	if err := devAutoInit(h, []string{"--no-open", work}); err != nil {
		t.Fatal(err)
	}
	cfg, err := localmode.Load(h.Dir)
	if err != nil {
		t.Fatalf("дом не создан: %v", err)
	}
	if st, err := os.Stat(work); err != nil || !st.IsDir() {
		t.Fatalf("папка проектов не создана: %v", err)
	}
	if cfg.WorkDir == "" || cfg.HostPaths != (runtime.GOOS != "windows") {
		t.Fatalf("config.yaml: %+v", cfg)
	}
	// второй раз — дом есть, ничего не меняется
	other := filepath.Join(t.TempDir(), "other")
	if err := devAutoInit(h, []string{other}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("при существующем доме авто-init не должен ничего создавать")
	}
}
