package localmode

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/arijjia/agctl/internal/localbridge"
)

// testRoot — изолированный корень: тесты не видят ~/.agctl пользователя.
func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	t.Setenv("AGCTL_ROOT", root)
	t.Setenv("AGCTL_DEV_HOME", "")
	t.Setenv("AGCTL_HOME_ID", "")
	return root
}

func initHome(t *testing.T, id string) {
	t.Helper()
	if err := (&Config{Profile: id}).Save(HomeDir(id)); err != nil {
		t.Fatal(err)
	}
}

func TestLayoutUnderOneRoot(t *testing.T) {
	root := testRoot(t)
	for got, want := range map[string]string{
		Root():          root,
		BinDir():        filepath.Join(root, "bin"),
		GuestDir():      filepath.Join(root, "bin", "guest"),
		ProfilesDir():   filepath.Join(root, "profiles"),
		HomeDir("utmn"): filepath.Join(root, "homes", "utmn"),
	} {
		if got != want {
			t.Errorf("%q, ждём %q", got, want)
		}
	}
	t.Setenv("AGCTL_ROOT", "")
	h, err := os.UserHomeDir()
	if err != nil {
		t.Skip("нет домашнего каталога")
	}
	if got := Root(); got != filepath.Join(h, ".agctl") {
		t.Fatalf("корень по умолчанию: %q", got)
	}
}

func TestSelectHomeOrder(t *testing.T) {
	testRoot(t)
	sel := func(flag string) Home {
		t.Helper()
		h, err := SelectHome(flag)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	// ничего нет — дом по умолчанию
	if h := sel(""); h.ID != "agctl" || h.Source != HomeFromDefault || h.Dir != HomeDir("agctl") {
		t.Fatalf("по умолчанию: %+v", h)
	}
	// единственный существующий дом
	initHome(t, "utmn")
	if h := sel(""); h.ID != "utmn" || h.Source != HomeFromSingle {
		t.Fatalf("единственный дом: %+v", h)
	}
	// домов несколько — снова по умолчанию
	initHome(t, "rshb")
	if h := sel(""); h.ID != "agctl" || h.Source != HomeFromDefault {
		t.Fatalf("несколько домов: %+v", h)
	}
	if got := ListHomes(); !reflect.DeepEqual(got, []string{"rshb", "utmn"}) {
		t.Fatalf("ListHomes: %v", got)
	}
	// AGCTL_HOME_ID сильнее
	t.Setenv("AGCTL_HOME_ID", "rshb")
	if h := sel(""); h.ID != "rshb" || h.Source != HomeFromEnvID {
		t.Fatalf("AGCTL_HOME_ID: %+v", h)
	}
	// AGCTL_DEV_HOME (путь) сильнее AGCTL_HOME_ID
	ext := filepath.Join(t.TempDir(), "My Home")
	t.Setenv("AGCTL_DEV_HOME", ext)
	if h := sel(""); h.Dir != ext || h.Source != HomeFromDevHome || h.ID != "rshb" {
		t.Fatalf("AGCTL_DEV_HOME: %+v", h)
	}
	t.Setenv("AGCTL_HOME_ID", "")
	if h := sel(""); h.ID != "my-home" {
		t.Fatalf("id дома вне корня — из имени каталога: %+v", h)
	}
	// флаг сильнее всего
	if h := sel("utmn"); h.ID != "utmn" || h.Source != HomeFromFlag || h.Dir != HomeDir("utmn") {
		t.Fatalf("--home: %+v", h)
	}
	for _, bad := range []string{"../x", "A", "a/b", ".hidden"} {
		if _, err := SelectHome(bad); err == nil {
			t.Errorf("--home %q принят", bad)
		}
	}
}

// Каждое поле Config записано в config.yaml со стандартным комментарием.
func TestConfigRenderDocumentsEveryField(t *testing.T) {
	documented := map[string]bool{}
	for _, d := range configDocs {
		documented[d.key] = true
		if d.what == "" || d.from == "" || d.who == "" {
			t.Errorf("поле %s: комментарий неполон", d.key)
		}
	}
	rt := reflect.TypeOf(Config{})
	for i := 0; i < rt.NumField(); i++ {
		key := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
		if !documented[key] {
			t.Errorf("поле %s (%s) не описано в configDocs", rt.Field(i).Name, key)
		}
	}
	if len(documented) != rt.NumField() {
		t.Errorf("configDocs: %d полей, в Config: %d", len(documented), rt.NumField())
	}
	raw, err := (&Config{}).Render("utmn")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, d := range configDocs {
		if !strings.Contains(text, "\n# "+d.key+" — ") || !strings.Contains(text, "\n"+d.key+":") {
			t.Errorf("в файле нет поля %s с комментарием", d.key)
		}
	}
	if strings.Count(text, "#   откуда: ") != len(configDocs) || strings.Count(text, "#   меняет: ") != len(configDocs) {
		t.Error("у каждого поля должны быть строки «откуда» и «меняет»")
	}
	if !strings.HasPrefix(text, "# agctl — локальный режим, дом «utmn».") {
		t.Errorf("шапка: %q", strings.SplitN(text, "\n", 2)[0])
	}
}

func TestConfigRoundtripAndLegacyDevYaml(t *testing.T) {
	testRoot(t)
	home := HomeDir("utmn")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := "profile: utmn\nimage: agctl-dsh-utmn:0.2.2\naddr: 127.0.0.1:8931\nworkdir: /src\nenv:\n  JIRA_URL: https://j\nsecrets: [GITLAB_TOKEN]\negress_allowlist: true\negress_allow: [\"1.1.1.1\"]\nbridge:\n  tools:\n    - name: host_x\n      command: [echo, \"{{a}}\"]\n      args:\n        a: {required: true}\n      confirm: true\n"
	if err := os.WriteFile(LegacyPath(home), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := Load(home)
	if err != nil {
		t.Fatalf("прежний dev.yaml не читается: %v", err)
	}
	if old.Profile != "utmn" || old.Addr != "127.0.0.1:8931" || len(old.Bridge.Tools) != 1 {
		t.Fatalf("%+v", old)
	}
	if got := ListHomes(); len(got) != 1 || got[0] != "utmn" {
		t.Fatalf("дом с dev.yaml считается настроенным: %v", got)
	}
	if err := old.Save(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LegacyPath(home)); !os.IsNotExist(err) {
		t.Fatal("dev.yaml должен уйти в dev.yaml.migrated")
	}
	if _, err := os.Stat(LegacyPath(home) + ".migrated"); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, got) {
		t.Fatalf("значения не сохранились:\n%+v\n%+v", old, got)
	}
}

func writeProfile(t *testing.T, dir, id, bridge string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte("schema: 1\nid: "+id+"\nbrand:\n  name: "+id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bridge != "" {
		if err := os.WriteFile(filepath.Join(dir, "local", "bridge-tools.yaml"), []byte(bridge), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveProfileOrder(t *testing.T) {
	root := testRoot(t)
	// встроенный
	p, ref, err := ResolveProfile(&Config{})
	if err != nil || p.ID != "agctl" || ref.Origin != ProfileFromEmbedded || ref.Dir != "" {
		t.Fatalf("встроенный: %+v %v", ref, err)
	}
	// неизвестный
	if _, _, err := ResolveProfile(&Config{Profile: "utmn"}); err == nil || !strings.Contains(err.Error(), filepath.Join(root, "profiles")) {
		t.Fatalf("ошибка должна назвать каталог профилей: %v", err)
	}
	// <корень>/profiles/<id> — в том числе симлинк на репо
	repo := filepath.Join(t.TempDir(), "agctl-profile-utmn")
	writeProfile(t, repo, "utmn", "")
	if err := os.MkdirAll(ProfilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, filepath.Join(ProfilesDir(), "utmn")); err != nil {
		t.Skipf("симлинки недоступны: %v", err)
	}
	p, ref, err = ResolveProfile(&Config{Profile: "utmn"})
	if err != nil || p.ID != "utmn" || ref.Origin != ProfileFromRoot || ref.Dir != filepath.Join(root, "profiles", "utmn") || ref.Link == "" {
		t.Fatalf("корень: %+v %v", ref, err)
	}
	// <корень>/profiles сильнее встроенного с тем же id
	writeProfile(t, filepath.Join(ProfilesDir(), "agctl"), "agctl", "")
	if _, ref, _ = ResolveProfile(&Config{Profile: "agctl"}); ref.Origin != ProfileFromRoot {
		t.Fatalf("дисковый agctl должен быть сильнее встроенного: %+v", ref)
	}
	// явный profile_dir сильнее всего
	explicit := filepath.Join(t.TempDir(), "p")
	writeProfile(t, explicit, "corp", "")
	p, ref, err = ResolveProfile(&Config{Profile: "utmn", ProfileDir: explicit})
	if err != nil || p.ID != "corp" || ref.Origin != ProfileFromConfig {
		t.Fatalf("profile_dir: %+v %v", ref, err)
	}
	// путь относительно cwd не ищется
	cwd := t.TempDir()
	writeProfile(t, filepath.Join(cwd, "profiles", "cwdorg"), "cwdorg", "")
	t.Chdir(cwd)
	if _, _, err := ResolveProfile(&Config{Profile: "cwdorg"}); err == nil {
		t.Fatal("профиль из ./profiles текущего каталога не должен находиться")
	}
	if _, _, err := ResolveProfile(&Config{Profile: "../x"}); err == nil {
		t.Fatal("id с обходом каталога принят")
	}
}

// Инструменты моста профиля читаются из профиля на каждом старте и
// объединяются с личными; в конфиг дома не копируются.
func TestBridgeToolsReadFromProfileEachTime(t *testing.T) {
	testRoot(t)
	dir := filepath.Join(ProfilesDir(), "utmn")
	writeProfile(t, dir, "utmn", "tools:\n  - name: org_a\n    command: [a]\n  - name: org_b\n    command: [b]\n")
	cfg := &Config{Profile: "utmn"}
	cfg.Bridge.Tools = []localbridge.Tool{{Name: "mine", Command: []string{"m"}}, {Name: "org_b", Command: []string{"override"}}}
	names := func() string {
		t.Helper()
		p, _, err := ResolveProfile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := EffectiveBridgeTools(cfg, p)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tl := range tools {
			out = append(out, tl.Name+"="+strings.Join(tl.Command, " "))
		}
		return strings.Join(out, ",")
	}
	if got := names(); got != "org_a=a,org_b=override,mine=m" {
		t.Fatalf("объединение: %s", got)
	}
	// профиль обновился — следующий старт видит новое без правки дома
	writeProfile(t, dir, "utmn", "tools:\n  - name: org_c\n    command: [c]\n")
	if got := names(); got != "org_c=c,mine=m,org_b=override" {
		t.Fatalf("после обновления профиля: %s", got)
	}
	if len(cfg.Bridge.Tools) != 2 {
		t.Fatal("личный список не должен меняться")
	}
	merged := MergeBridgeTools([]localbridge.Tool{{Name: "x"}}, []localbridge.Tool{{Name: "y"}})
	if merged[0].Personal || !merged[1].Personal {
		t.Fatalf("источник: %+v", merged)
	}
}

// Секреты хранилища ОС: свой сервис у каждого дома, чтение-фолбэк из
// прежнего общего agctl-dev.
func TestKeyringServicePerHomeWithLegacyFallback(t *testing.T) {
	testRoot(t)
	t.Setenv("AGCTL_DEV_SECRETS", "")
	keyring.MockInit()
	utmn, rshb := OpenStore(HomeDir("utmn")), OpenStore(HomeDir("rshb"))
	if utmn.Fallback() || utmn.Service() != "agctl:utmn" || rshb.Service() != "agctl:rshb" {
		t.Fatalf("сервисы: %q %q", utmn.Service(), rshb.Service())
	}
	if !strings.Contains(utmn.Backend(), "agctl:utmn") {
		t.Fatalf("backend: %s", utmn.Backend())
	}
	if err := utmn.Set("GITLAB_TOKEN", "utmn-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := rshb.Get("GITLAB_TOKEN"); err != ErrSecretNotFound {
		t.Fatalf("секрет дома utmn виден в rshb: %v", err)
	}
	if v, _ := keyring.Get("agctl:utmn", "GITLAB_TOKEN"); v != "utmn-token" {
		t.Fatal("запись должна лежать в сервисе дома")
	}
	// легаси: читается обоими домами, пока своего значения нет
	if err := keyring.Set(LegacyKeyringService, "JIRA_API_TOKEN", "legacy"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{utmn, rshb} {
		if v, err := s.Get("JIRA_API_TOKEN"); err != nil || v != "legacy" {
			t.Fatalf("фолбэк: %q %v", v, err)
		}
	}
	if err := utmn.Set("JIRA_API_TOKEN", "own"); err != nil {
		t.Fatal(err)
	}
	if v, _ := utmn.Get("JIRA_API_TOKEN"); v != "own" {
		t.Fatal("своё значение сильнее легаси")
	}
	if v, _ := keyring.Get(LegacyKeyringService, "JIRA_API_TOKEN"); v != "legacy" {
		t.Fatal("запись в легаси-сервис недопустима")
	}
	// удаление в доме не воскрешает легаси и не трогает его
	if err := utmn.Delete("JIRA_API_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, err := utmn.Get("JIRA_API_TOKEN"); err != ErrSecretNotFound {
		t.Fatalf("после удаления всплыло легаси-значение: %v", err)
	}
	if v, err := rshb.Get("JIRA_API_TOKEN"); err != nil || v != "legacy" {
		t.Fatal("удаление в utmn не должно задевать rshb")
	}
	// перенос: копия в сервис дома, исходник цел
	moved, err := rshb.ImportLegacy([]string{"JIRA_API_TOKEN", "NOPE"})
	if err != nil || len(moved) != 1 {
		t.Fatalf("ImportLegacy: %v %v", moved, err)
	}
	if v, _ := keyring.Get("agctl:rshb", "JIRA_API_TOKEN"); v != "legacy" {
		t.Fatal("значение не скопировано в сервис дома")
	}
}
