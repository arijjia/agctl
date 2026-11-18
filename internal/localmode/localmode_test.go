package localmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/localbridge"
)

func fileStore(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv("AGCTL_DEV_SECRETS", "file") // тесты не трогают хранилище ОС
	home := t.TempDir()
	return OpenStore(home), home
}

func TestFileStoreRoundtrip(t *testing.T) {
	s, home := fileStore(t)
	if !strings.Contains(s.Backend(), "secrets.enc") {
		t.Fatalf("backend: %s", s.Backend())
	}
	if _, err := s.Get("GITLAB_TOKEN"); err != ErrSecretNotFound {
		t.Fatalf("пустое хранилище: %v", err)
	}
	if err := s.Set("GITLAB_TOKEN", "glpat-secret-value"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "secrets.enc"))
	if strings.Contains(string(raw), "glpat-secret-value") {
		t.Fatal("значение в файле должно быть зашифровано")
	}
	info, _ := os.Stat(filepath.Join(home, "secrets.enc"))
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("права secrets.enc: %v", info.Mode().Perm())
	}
	if v, err := OpenStore(home).Get("GITLAB_TOKEN"); err != nil || v != "glpat-secret-value" {
		t.Fatalf("повторное открытие: %q %v", v, err)
	}
	if err := s.Set("bad-name", "x"); err == nil {
		t.Fatal("имя переменной валидируется")
	}
	if err := s.Set("X", "a\nb"); err == nil {
		t.Fatal("многострочные значения отклоняются")
	}
	if err := s.Delete("GITLAB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("GITLAB_TOKEN"); err != ErrSecretNotFound {
		t.Fatal("удалённый секрет")
	}
}

func TestContainerEnvAliasesAndNoLLMKey(t *testing.T) {
	s, _ := fileStore(t)
	_ = s.Set("GITLAB_TOKEN", "gl-tok")
	_ = s.Set("JIRA_API_TOKEN", "jira-tok")
	_ = s.Set("AGCTL_LLM_API_KEY", "llm-key")
	cfg := &Config{
		Env:     map[string]string{"GITLAB_URL": "https://gitlab.example.org/", "JIRA_URL": "https://jira.example.org", "JIRA_USER": "ivanov"},
		Secrets: []string{"GITLAB_TOKEN", "JIRA_API_TOKEN", "AGCTL_LLM_API_KEY", "TAVILY_API_KEY"},
	}
	env, missing := ContainerEnv(cfg, s)
	got := strings.Join(env, "\n")
	for _, want := range []string{
		"GITLAB_TOKEN=gl-tok", "GITLAB_HOST=gitlab.example.org", "GITLAB_API_URL=https://gitlab.example.org/api/v4",
		"GITLAB_PERSONAL_ACCESS_TOKEN=gl-tok", "JIRA_API_TOKEN=jira-tok", "JIRA_PERSONAL_TOKEN=jira-tok",
		"JIRA_USERNAME=ivanov",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %s в\n%s", want, got)
		}
	}
	if strings.Contains(got, "llm-key") {
		t.Fatal("ключ LLM в env контейнера не попадает (его получает сервер как провайдер)")
	}
	if len(missing) != 1 || missing[0] != "TAVILY_API_KEY" {
		t.Fatalf("missing=%v", missing)
	}
}

func TestConfigSaveLoadAndBridgeValidation(t *testing.T) {
	home := t.TempDir()
	if _, err := Load(home); err != ErrNotInitialized {
		t.Fatalf("нет dev.yaml: %v", err)
	}
	c := &Config{Profile: "agctl", Image: "agctl-dsh:0.2.2", Env: map[string]string{"JIRA_URL": "https://j"}}
	c.Bridge.Tools = []localbridge.Tool{{Name: "ok", Command: []string{"echo"}}}
	if err := c.Save(home); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != DefaultAddr || got.Image != "agctl-dsh:0.2.2" || len(got.Bridge.Tools) != 1 {
		t.Fatalf("%+v", got)
	}
	raw, _ := os.ReadFile(Path(home))
	if !strings.Contains(string(raw), "Секретов здесь нет") {
		t.Fatal("шапка dev.yaml")
	}
	_ = os.WriteFile(Path(home), []byte("bridge:\n  tools:\n    - name: Bad\n      command: [x]\n"), 0o600)
	if _, err := Load(home); err == nil {
		t.Fatal("невалидный белый список моста не загружается молча")
	}
}

func TestValidateWorkDir(t *testing.T) {
	dir := t.TempDir()
	if got, err := ValidateWorkDir(dir); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("%q %v", got, err)
	}
	if h, err := os.UserHomeDir(); err == nil {
		if _, err := ValidateWorkDir(h); err == nil {
			t.Fatal("домашний каталог целиком запрещён")
		}
	}
	if _, err := ValidateWorkDir(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("несуществующая папка")
	}
	if _, err := ValidateWorkDir(""); err == nil {
		t.Fatal("пустая папка")
	}
	root := filepath.VolumeName(dir) + string(filepath.Separator)
	if _, err := ValidateWorkDir(root); err == nil {
		t.Fatal("корень ФС запрещён")
	}
}

func TestBackupRestoreAndPrune(t *testing.T) {
	home := t.TempDir()
	if p, err := Backup(home, "x"); err != nil || p != "" {
		t.Fatalf("без dsh-home бэкап пуст: %q %v", p, err)
	}
	dsh := DshHome(home)
	_ = os.MkdirAll(filepath.Join(dsh, "sessions"), 0o755)
	_ = os.WriteFile(filepath.Join(dsh, "sessions", "a.json"), []byte("v1"), 0o644)
	arch, err := Backup(home, "before-agctl-dsh:0.2.3")
	if err != nil || arch == "" {
		t.Fatal(err)
	}
	if strings.ContainsAny(filepath.Base(arch), ":/") {
		t.Fatalf("метка очищена: %s", arch)
	}
	_ = os.WriteFile(filepath.Join(dsh, "sessions", "a.json"), []byte("v2-broken"), 0o644)
	if err := Restore(home, arch); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dsh, "sessions", "a.json"))
	if string(raw) != "v1" {
		t.Fatalf("восстановлено %q", raw)
	}
	matches, _ := filepath.Glob(dsh + ".before-restore-*")
	if len(matches) != 1 {
		t.Fatal("текущий dsh-home не удаляется молча, а откладывается")
	}
	for i := 0; i < KeepBackups+2; i++ {
		_ = os.WriteFile(filepath.Join(BackupsDir(home), "dsh-home-2000010"+string(rune('0'+i))+"-000000-x.tar.gz"), []byte("x"), 0o600)
	}
	_, _ = Backup(home, "y")
	if n := len(Backups(home)); n != KeepBackups {
		t.Fatalf("хранится %d бэкапов, ожидалось %d", n, KeepBackups)
	}
}

func TestStateRoundtrip(t *testing.T) {
	home := t.TempDir()
	if st, err := ReadState(home); st != nil || err != nil {
		t.Fatal("нет состояния")
	}
	if err := WriteState(home, State{PID: 42, Addr: DefaultAddr, ControlToken: NewToken()}); err != nil {
		t.Fatal(err)
	}
	st, err := ReadState(home)
	if err != nil || st.PID != 42 || len(st.ControlToken) < 40 {
		t.Fatalf("%+v %v", st, err)
	}
	ClearState(home)
	if st, _ := ReadState(home); st != nil {
		t.Fatal("ClearState")
	}
}

func TestClearOwnStateKeepsForeign(t *testing.T) {
	home := t.TempDir()
	_ = WriteState(home, State{PID: 111})
	ClearOwnState(home, 222)
	if st, _ := ReadState(home); st == nil {
		t.Fatal("упавший второй экземпляр не стирает состояние живого")
	}
	ClearOwnState(home, 111)
	if st, _ := ReadState(home); st != nil {
		t.Fatal("свой файл состояния убирается")
	}
}
