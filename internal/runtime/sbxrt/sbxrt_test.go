package sbxrt

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// Настоящего sbx в тестах нет: тестовый бинарь сам играет роль CLI, когда
// задан SBXRT_FAKE_DIR (каталог состояния подделки). Подделка ведёт журнал
// вызовов и повторяет только документированное поведение команд.
func TestMain(m *testing.M) {
	if dir := os.Getenv("SBXRT_FAKE_DIR"); dir != "" {
		os.Exit(fakeSbx(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSbx(dir string, args []string) int {
	logf, _ := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintln(logf, strings.Join(args, " "))
	logf.Close()
	if len(args) > 0 && args[0] == "--no-input" {
		args = args[1:]
	}
	flagValue := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	sandbox := func(name string) string { return filepath.Join(dir, "sandbox-"+name) }
	has := func(prefix ...string) bool {
		return len(args) >= len(prefix) && strings.Join(args[:len(prefix)], " ") == strings.Join(prefix, " ")
	}
	fail := func(msg string) int { fmt.Fprintln(os.Stderr, "Error: "+msg); return 1 }
	switch {
	case has("version"):
		fmt.Println("sbx version 0.0.0-fake")
	case has("ls", "-q"):
		if os.Getenv("SBXRT_FAKE_LOGGED_OUT") != "" {
			return fail("not signed in; run: sbx login")
		}
		matches, _ := filepath.Glob(sandbox("*"))
		for _, m := range matches {
			fmt.Println(strings.TrimPrefix(filepath.Base(m), "sandbox-"))
		}
	case has("template", "ls", "-q"):
		raw, _ := os.ReadFile(filepath.Join(dir, "templates"))
		fmt.Print(string(raw))
	case has("template", "load"):
		ref, err := os.ReadFile(args[2])
		if err != nil {
			return fail(err.Error())
		}
		f, _ := os.OpenFile(filepath.Join(dir, "templates"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, string(ref))
		f.Close()
	case has("create"):
		name := flagValue("--name")
		if _, err := os.Stat(sandbox(name)); err == nil {
			return fail("sandbox already exists")
		}
		_ = os.WriteFile(sandbox(name), nil, 0o600)
	case has("policy", "allow", "network"):
	case has("secret", "set-custom"):
		value, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(filepath.Join(dir, "secret-"+flagValue("--name")), value, 0o600)
	case has("port", "publish"):
		_ = os.WriteFile(filepath.Join(dir, "port-"+args[2]), []byte(strings.TrimSuffix(args[3], ":3080")), 0o600)
	case has("port", "unpublish"):
		if os.Remove(filepath.Join(dir, "port-"+args[2])) != nil {
			return fail("port is not published")
		}
	case has("stop"):
		if _, err := os.Stat(sandbox(args[1])); err != nil {
			return fail("no such sandbox")
		}
	case has("rm", "--force"):
		_ = os.Remove(sandbox(args[2]))
	case has("exec"):
		name := args[len(args)-2]
		env, _ := os.ReadFile(flagValue("--env-file"))
		_ = os.WriteFile(filepath.Join(dir, "env-"+name), env, 0o600)
		if os.Getenv("SBXRT_FAKE_DSH_CRASH") != "" {
			fmt.Println("[agctl-entrypoint] dsh: boom")
			return 3
		}
		addr, _ := os.ReadFile(filepath.Join(dir, "port-"+name))
		ln, err := net.Listen("tcp", string(addr))
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println("dsh web: http://172.17.0.2:3080/?token=boot-token-1")
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "dsh") }))
	default:
		return fail("fake sbx: unknown command " + strings.Join(args, " "))
	}
	return 0
}

type fakeEnv struct {
	t    *testing.T
	dir  string
	home string
	rt   *Runtime
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	e := &fakeEnv{t: t, dir: t.TempDir(), home: t.TempDir()}
	t.Setenv("SBXRT_FAKE_DIR", e.dir)
	old := hostSupport
	hostSupport = func() error { return nil }
	t.Cleanup(func() { hostSupport = old })
	rt, err := New(context.Background(), Config{
		Bin: os.Args[0], Home: e.home, Image: "agctl-dsh-test:1",
		MemoryBytes: 4 << 30, NanoCPUs: 1_500_000_000,
		DshSkel: assets.DshSkel, Allow: []string{"nexus.example.org"},
		ReadyTimeout: 20 * time.Second,
		ImageID:      func(context.Context, string) (string, error) { return "sha256:one", nil },
		SaveImage: func(_ context.Context, ref, path string) error {
			return os.WriteFile(path, []byte(ref), 0o600)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	e.rt = rt
	return e
}

func (e *fakeEnv) calls(prefix string) []string {
	raw, _ := os.ReadFile(filepath.Join(e.dir, "calls.log"))
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "--no-input "+prefix) {
			out = append(out, line)
		}
	}
	return out
}

func (e *fakeEnv) spec(workdir string) agctlrt.ChatSpec {
	return agctlrt.ChatSpec{
		RuntimeID: "agctl-chat-ab12-u1", StorageID: "u1", UserID: 1, Local: true, WorkDir: workdir,
		Provider: provision.Provider{BaseURL: "https://llm.example.org/v1", Model: "m", Models: []string{"m"}, APIKey: "sk-REAL-KEY"},
		DevEnv: []string{
			"GITLAB_URL=https://git.example.org",
			"AGCTL_BRIDGE_URL=http://host.docker.internal:8930",
			"AGCTL_LLM_API_KEY=sk-REAL-KEY",
		},
	}
}

func TestEnsureChatLifecycle(t *testing.T) {
	e := newFakeEnv(t)
	ctx := context.Background()
	work := t.TempDir()
	h, err := e.rt.EnsureChat(ctx, e.spec(work))
	if err != nil {
		t.Fatal(err)
	}
	if h.InstanceID != InstancePrefix+"agctl-chat-ab12-u1" || !strings.HasPrefix(h.Endpoint, "http://127.0.0.1:") {
		t.Fatalf("handle: %+v", h)
	}
	resp, err := http.Get(h.Endpoint)
	if err != nil {
		t.Fatalf("dsh web не отвечает: %v", err)
	}
	resp.Body.Close()

	base := filepath.Join(e.home, "chats", "u1")
	create := e.calls("create")
	want := "--no-input create --name agctl-chat-ab12-u1 --template agctl-dsh-test:1 --pull never --cpus 2 --memory 4096m shell " +
		filepath.Join(base, "sbx") + " " + filepath.Join(base, "dsh-home") + ":/home/dsh " + work + ":/workspace"
	if len(create) != 1 || create[0] != want {
		t.Fatalf("create:\n got %q\nwant %q", create, want)
	}
	if got := e.calls("template load"); len(got) != 1 {
		t.Fatalf("шаблон должен загрузиться один раз: %q", got)
	}
	policy := e.calls("policy allow network")
	if len(policy) != 1 || !strings.HasSuffix(policy[0], "--sandbox agctl-chat-ab12-u1 git.example.org:443,llm.example.org:443,localhost:8930,nexus.example.org") {
		t.Fatalf("policy: %q", policy)
	}

	// Ключ модели: в sbx — через stdin; в аргументах, окружении и
	// .credentials.yaml песочницы — только заглушка.
	raw, _ := os.ReadFile(filepath.Join(e.dir, "calls.log"))
	if strings.Contains(string(raw), "sk-REAL-KEY") {
		t.Fatalf("ключ модели попал в аргументы sbx:\n%s", raw)
	}
	if got, _ := os.ReadFile(filepath.Join(e.dir, "secret-agctl-llm-main")); string(got) != "sk-REAL-KEY" {
		t.Fatalf("прокси sbx не получил ключ: %q", got)
	}
	secret := e.calls("secret set-custom")
	if len(secret) != 1 || !strings.Contains(secret[0], "--host llm.example.org") || !strings.Contains(secret[0], "--placeholder agctl-sbx-") {
		t.Fatalf("secret: %q", secret)
	}
	envFile, _ := os.ReadFile(filepath.Join(e.dir, "env-agctl-chat-ab12-u1"))
	creds, _ := os.ReadFile(filepath.Join(base, "dsh-home", ".credentials.yaml"))
	for what, content := range map[string]string{"env": string(envFile), ".credentials.yaml": string(creds)} {
		if strings.Contains(content, "sk-REAL-KEY") || !strings.Contains(content, "agctl-sbx-") {
			t.Fatalf("%s: ожидалась заглушка вместо ключа:\n%s", what, content)
		}
	}
	if !strings.Contains(string(envFile), "DSH_HOME=/home/dsh\n") || !strings.Contains(string(envFile), "GITLAB_URL=https://git.example.org\n") {
		t.Fatalf("env: %s", envFile)
	}

	logs, err := e.rt.LogsChat(ctx, h, "80")
	if err != nil || !strings.Contains(logs, "token=boot-token-1") {
		t.Fatalf("logs: %q %v", logs, err)
	}
	if st, _ := e.rt.HealthStatus(ctx, h.InstanceID); st != "healthy" {
		t.Fatalf("health: %s", st)
	}

	// Сон: sbx stop, процесс dsh снят; пробуждение — без нового create и
	// без повторной загрузки шаблона.
	if err := e.rt.StopChat(ctx, h); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.rt.HealthStatus(ctx, h.InstanceID); st != "unhealthy" {
		t.Fatalf("health после остановки: %s", st)
	}
	if got := e.calls("stop agctl-chat-ab12-u1"); len(got) != 1 {
		t.Fatalf("stop: %q", got)
	}
	h2, err := e.rt.EnsureChat(ctx, e.spec(work))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.calls("create")) != 1 || len(e.calls("template load")) != 1 || len(e.calls("rm")) != 0 {
		t.Fatalf("пробуждение не должно пересоздавать песочницу: create=%q rm=%q", e.calls("create"), e.calls("rm"))
	}
	if resp, err := http.Get(h2.Endpoint); err != nil {
		t.Fatalf("после пробуждения dsh web не отвечает: %v", err)
	} else {
		resp.Body.Close()
	}

	// Другая папка проектов — маунты фиксируются при создании: пересоздание.
	if _, err := e.rt.EnsureChat(ctx, e.spec(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if len(e.calls("rm --force agctl-chat-ab12-u1")) != 1 || len(e.calls("create")) != 2 {
		t.Fatalf("смена маунтов должна пересоздать песочницу: rm=%q create=%q", e.calls("rm"), e.calls("create"))
	}
}

func TestEnsureChatMounts(t *testing.T) {
	e := newFakeEnv(t)
	a, b := t.TempDir(), t.TempDir()
	spec := e.spec("")
	spec.Mounts = []agctlrt.Mount{{Host: a, Name: "api"}, {Host: b, Name: "web"}}
	if _, err := e.rt.EnsureChat(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	create := e.calls("create")
	work := filepath.Join(e.home, "chats", "u1", "work")
	if len(create) != 1 || !strings.HasSuffix(create[0], " "+work+":/workspace "+a+":/workspace/api "+b+":/workspace/web") {
		t.Fatalf("create: %q", create)
	}
	spec.Mounts = append(spec.Mounts, agctlrt.Mount{Host: a, Name: "api"})
	if _, err := e.rt.EnsureChat(context.Background(), spec); err == nil {
		t.Fatal("дубликат имени маунта должен отклоняться")
	}
}

func TestEnsureChatDshCrash(t *testing.T) {
	e := newFakeEnv(t)
	t.Setenv("SBXRT_FAKE_DSH_CRASH", "1")
	_, err := e.rt.EnsureChat(context.Background(), e.spec(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "dsh: boom") {
		t.Fatalf("падение dsh при старте должно быть явной ошибкой с логом: %v", err)
	}
	if got := e.calls("stop"); len(got) != 1 {
		t.Fatalf("после неудачи песочница должна быть остановлена: %q", got)
	}
}

func TestProbe(t *testing.T) {
	old := hostSupport
	hostSupport = func() error { return nil }
	t.Cleanup(func() { hostSupport = old })
	if _, err := Probe(context.Background(), filepath.Join(t.TempDir(), "no-such-sbx")); err == nil || !strings.Contains(err.Error(), "не установлен") {
		t.Fatalf("нет бинаря: %v", err)
	}
	t.Setenv("SBXRT_FAKE_DIR", t.TempDir())
	version, err := Probe(context.Background(), os.Args[0])
	if err != nil || version != "sbx version 0.0.0-fake" {
		t.Fatalf("probe: %q %v", version, err)
	}
	t.Setenv("SBXRT_FAKE_LOGGED_OUT", "1")
	if _, err := Probe(context.Background(), os.Args[0]); err == nil || !strings.Contains(err.Error(), "sbx login") {
		t.Fatalf("без входа: %v", err)
	}
}

func TestPlan(t *testing.T) {
	for in, want := range map[string]string{
		"agctl-chat-AB_12-u1":   "agctl-chat-ab-12-u1",
		"x":                     "agctl-x",
		"default":               "agctl-default",
		strings.Repeat("a", 70): strings.Repeat("a", 63),
	} {
		if got := SandboxName(in); got != want {
			t.Errorf("SandboxName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"https://llm.example.org/v1":       "llm.example.org:443",
		"http://host.docker.internal:4000": "localhost:4000",
		"http://10.0.0.5":                  "10.0.0.5:80",
		"glpat-token":                      "",
		"/home/dsh/creds":                  "",
	} {
		if got := urlResource(in); got != want {
			t.Errorf("urlResource(%q) = %q, want %q", in, got, want)
		}
	}
	if !templateListed("a:1\nagctl-dsh\n", "agctl-dsh:0.2.2") || !templateListed("reg:5000/x/agctl-dsh:0.2.2\n", "reg:5000/x/agctl-dsh:0.2.2") || templateListed("other:1\n", "agctl-dsh:0.2.2") {
		t.Error("templateListed")
	}
	if _, err := envFileContent([]string{"A=1\n2"}, nil); err == nil {
		t.Error("перевод строки в значении должен отклоняться")
	}
}

// stubProvider — «Docker» для Switch.
type stubProvider struct{ ensured, stopped int }

func (s *stubProvider) EnsureChat(context.Context, agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	s.ensured++
	return agctlrt.ChatHandle{RuntimeID: "r", InstanceID: "c0ffee", Endpoint: "http://127.0.0.1:1"}, nil
}
func (s *stubProvider) StopChat(context.Context, agctlrt.ChatHandle) error { s.stopped++; return nil }
func (s *stubProvider) LogsChat(context.Context, agctlrt.ChatHandle, string) (string, error) {
	return "docker", nil
}
func (s *stubProvider) HasActiveSSH(context.Context, agctlrt.ChatHandle) (bool, error) {
	return true, nil
}

func TestSwitch(t *testing.T) {
	e := newFakeEnv(t)
	ctx := context.Background()
	docker := &stubProvider{}
	kind, created := KindDocker, 0
	sw := &Switch{Docker: docker, Kind: func() string { return kind }, NewSbx: func(context.Context) (*Runtime, error) {
		created++
		return e.rt, nil
	}}
	hd, err := sw.EnsureChat(ctx, e.spec(t.TempDir()))
	if err != nil || docker.ensured != 1 || created != 0 {
		t.Fatalf("docker по умолчанию, sbx не трогаем: %v ensured=%d created=%d", err, docker.ensured, created)
	}
	kind = KindSbx
	hs, err := sw.EnsureChat(ctx, e.spec(t.TempDir()))
	if err != nil || !strings.HasPrefix(hs.InstanceID, InstancePrefix) || docker.ensured != 1 {
		t.Fatalf("sbx: %+v %v", hs, err)
	}
	// Остановка идёт в рантайм, создавший место, а не в выбранный сейчас.
	kind = KindDocker
	if err := sw.StopChat(ctx, hs); err != nil || docker.stopped != 0 || len(e.calls("stop")) != 1 {
		t.Fatalf("stop sbx: %v", err)
	}
	if err := sw.StopChat(ctx, hd); err != nil || docker.stopped != 1 {
		t.Fatalf("stop docker: %v", err)
	}
	if ssh, _ := sw.HasActiveSSH(ctx, hs); ssh {
		t.Fatal("в sbx SSH нет")
	}
	if logs, _ := sw.LogsChat(ctx, hd, "1"); logs != "docker" {
		t.Fatalf("logs: %q", logs)
	}
	if created != 1 {
		t.Fatalf("рантайм sbx создаётся один раз: %d", created)
	}
}
