package axrt

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// fakeAX — in-memory ax-server: задачи создаются Suspended, resume → Running
// + Ready=True, suspend → Suspended.
type fakeAX struct {
	v1alpha1.UnimplementedAXServer
	mu      sync.Mutex
	tasks   map[string]*v1alpha1.Task
	resumes int
	deletes int
}

func newFakeAX() *fakeAX { return &fakeAX{tasks: map[string]*v1alpha1.Task{}} }

func key(atespace, name string) string { return atespace + "/" + name }

func (f *fakeAX) GetTask(_ context.Context, req *v1alpha1.GetTaskRequest) (*v1alpha1.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[key(req.Atespace, req.Name)]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "task %q not found", req.Name)
	}
	return proto.Clone(task).(*v1alpha1.Task), nil
}

func (f *fakeAX) CreateTask(_ context.Context, req *v1alpha1.CreateTaskRequest) (*v1alpha1.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task := proto.Clone(req.Task).(*v1alpha1.Task)
	k := key(task.Metadata.Atespace, task.Metadata.Name)
	if _, exists := f.tasks[k]; exists {
		return nil, status.Error(codes.FailedPrecondition, "already exists and is immutable")
	}
	task.Status = &v1alpha1.TaskStatus{Phase: v1alpha1.PhaseSuspended, Id: "id-" + task.Metadata.Name}
	f.tasks[k] = task
	return proto.Clone(task).(*v1alpha1.Task), nil
}

func (f *fakeAX) ResumeTask(_ context.Context, req *v1alpha1.ResumeTaskRequest) (*v1alpha1.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[key(req.Atespace, req.Name)]
	if !ok {
		return nil, status.Error(codes.NotFound, "no task")
	}
	f.resumes++
	task.Status.Phase = v1alpha1.PhaseRunning
	task.Status.WorkerIp = "10.20.0.7"
	task.Status.Conditions = []*v1alpha1.Condition{{Type: v1alpha1.ConditionReady, Status: "True", Reason: "TaskRunning"}}
	return proto.Clone(task).(*v1alpha1.Task), nil
}

func (f *fakeAX) SuspendTask(_ context.Context, req *v1alpha1.SuspendTaskRequest) (*v1alpha1.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[key(req.Atespace, req.Name)]
	if !ok {
		return nil, status.Error(codes.NotFound, "no task")
	}
	task.Status.Phase = v1alpha1.PhaseSuspended
	task.Status.Conditions = []*v1alpha1.Condition{{Type: v1alpha1.ConditionReady, Status: "False", Reason: "Suspended"}}
	return proto.Clone(task).(*v1alpha1.Task), nil
}

func (f *fakeAX) DeleteTask(_ context.Context, req *v1alpha1.DeleteTaskRequest) (*v1alpha1.DeleteTaskResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, key(req.Atespace, req.Name))
	f.deletes++
	return &v1alpha1.DeleteTaskResponse{}, nil
}

func (f *fakeAX) ListTasks(_ context.Context, req *v1alpha1.ListTasksRequest) (*v1alpha1.ListTasksResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := &v1alpha1.ListTasksResponse{}
	for k, task := range f.tasks {
		if strings.HasPrefix(k, req.Atespace+"/") {
			resp.Tasks = append(resp.Tasks, proto.Clone(task).(*v1alpha1.Task))
		}
	}
	return resp, nil
}

func (f *fakeAX) task(atespace, name string) *v1alpha1.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks[key(atespace, name)]
}

type routerRecord struct {
	mu      sync.Mutex
	headers []http.Header
	paths   []string
}

func startFixture(t *testing.T) (*fakeAX, v1alpha1.AXClient, *httptest.Server, *routerRecord) {
	t.Helper()
	fake := newFakeAX()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	v1alpha1.RegisterAXServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rec := &routerRecord{}
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.headers = append(rec.headers, r.Header.Clone())
		rec.paths = append(rec.paths, r.URL.RequestURI())
		rec.mu.Unlock()
		if r.URL.Path == BootLogPath {
			if r.Header.Get("Authorization") != "Bearer runner-secret" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte("dsh web listening on http://10.20.0.7:3080/?token=boot-abc\n"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(router.Close)
	return fake, v1alpha1.NewAXClient(conn), router, rec
}

func testConfig(router string) Config {
	return Config{
		Server: "unused", Atespace: "agctl", Image: "reg.invalid/agctl-dsh@sha256:0123456789abcdef",
		RouterURL: router, RunnerToken: "runner-secret", Version: "0.9.0",
		ReadyTimeout: 5 * time.Second, InjectLLMKey: true, CPU: "2", Memory: "2Gi",
	}
}

func spec() agctlrt.ChatSpec {
	return agctlrt.ChatSpec{
		RuntimeID: "chat-abcd1234-u17", StorageID: "u17", UserID: 17, DefaultFullAccess: true,
		Provider: provision.Provider{BaseURL: "https://llm.example.invalid/v1", Model: "m", Models: []string{"m"}, APIKey: "unit-key"},
	}
}

func envOf(task *v1alpha1.Task) map[string]string {
	out := map[string]string{}
	for _, e := range task.GetSpec().GetEnv() {
		out[e.Name] = e.Value
	}
	return out
}

func TestEnsureChatCreatesResumesAndRoutesThroughHeader(t *testing.T) {
	fake, client, router, rec := startFixture(t)
	provider, err := NewWithClient(testConfig(router.URL), client, router.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	handle, err := provider.EnsureChat(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	name := TaskName("chat-abcd1234-u17")
	if handle.Endpoint != router.URL || handle.Headers[HeaderTargetActor] != "agctl/"+name {
		t.Fatalf("handle must point at the router with the actor header: %+v", handle)
	}
	if !strings.HasPrefix(handle.InstanceID, name+"/id-") || !strings.HasSuffix(handle.InstanceID, "@10.20.0.7") {
		t.Fatalf("instance id = %q", handle.InstanceID)
	}
	task := fake.task("agctl", name)
	if task.Spec.Image != testConfig("").Image || task.Spec.Command[0] != "/usr/local/bin/agctl-entrypoint" {
		t.Fatalf("task spec: %+v", task.Spec)
	}
	env := envOf(task)
	for k, want := range map[string]string{
		"AGCTL_LLM_API_KEY": "unit-key", "AGCTL_LLM_BASE_URL": "https://llm.example.invalid/v1",
		"DSH_HOME": "/workspace/dsh-home", "HOME": "/workspace/work", RunnerTokenEnv: "runner-secret",
		"AGCTL_DEFAULT_FULL_ACCESS": "1", "AGCTL_WS_ID": "chat-abcd1234-u17",
	} {
		if env[k] != want {
			t.Fatalf("env %s = %q, want %q", k, env[k], want)
		}
	}
	if task.Spec.Resources.Limits.Cpu != "2" || task.Spec.Resources.Limits.Memory != "2Gi" {
		t.Fatalf("limits: %+v", task.Spec.Resources)
	}
	if fields := task.Spec.Config.GetFields(); fields[configKeyUserID].GetStringValue() != "17" || fields[configKeySpecHash].GetStringValue() == "" {
		t.Fatalf("config: %v", fields)
	}
	if fake.resumes != 1 {
		t.Fatalf("resumes = %d, want 1", fake.resumes)
	}
	// Повторный Ensure для Running-задачи не делает resume.
	if _, err := provider.EnsureChat(ctx, spec()); err != nil {
		t.Fatal(err)
	}
	if fake.resumes != 1 {
		t.Fatalf("running task must not be resumed again: %d", fake.resumes)
	}

	logs, err := provider.LogsChat(ctx, handle, "80")
	if err != nil || !strings.Contains(logs, "token=boot-abc") {
		t.Fatalf("boot log via router: %q %v", logs, err)
	}
	rec.mu.Lock()
	last := rec.headers[len(rec.headers)-1]
	path := rec.paths[len(rec.paths)-1]
	rec.mu.Unlock()
	if last.Get(HeaderTargetActor) != "agctl/"+name || path != BootLogPath+"?tail=80" {
		t.Fatalf("boot log request: %v %s", last, path)
	}

	if err := provider.StopChat(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if fake.task("agctl", name).Status.Phase != v1alpha1.PhaseSuspended {
		t.Fatal("StopChat must suspend, not delete, the task")
	}
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "healthy" {
		t.Fatalf("suspended task health = %q", st)
	}
	// Следующий Ensure — resume спящей задачи.
	if _, err := provider.EnsureChat(ctx, spec()); err != nil {
		t.Fatal(err)
	}
	if fake.resumes != 2 {
		t.Fatalf("suspended task must be resumed: %d", fake.resumes)
	}
	list, err := provider.ChatContainers(ctx)
	if err != nil || len(list) != 1 || list[0].UserID != 17 || list[0].Workspace != "chat-abcd1234-u17" || list[0].State != "running" {
		t.Fatalf("ChatContainers = %+v %v", list, err)
	}
}

func TestKeysStayOutOfTaskWhenEgressInjectionIsUsed(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.InjectLLMKey = false
	provider, err := NewWithClient(cfg, client, router.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.EnsureChat(context.Background(), spec()); err != nil {
		t.Fatal(err)
	}
	env := envOf(fake.task("agctl", TaskName("chat-abcd1234-u17")))
	if _, leaked := env["AGCTL_LLM_API_KEY"]; leaked {
		t.Fatal("API key must not reach the task env in egress-injection mode")
	}
	if env["AGCTL_LLM_BASE_URL"] == "" {
		t.Fatal("provider endpoint must still be configured")
	}
}

func TestDriftKeepsImmutableTaskUnlessRecreateRequested(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	provider, _ := NewWithClient(cfg, client, router.Client())
	ctx := context.Background()
	if _, err := provider.EnsureChat(ctx, spec()); err != nil {
		t.Fatal(err)
	}
	changed := spec()
	changed.Provider.Model = "other-model"
	if _, err := provider.EnsureChat(ctx, changed); err != nil {
		t.Fatal(err)
	}
	name := TaskName("chat-abcd1234-u17")
	if fake.deletes != 0 || envOf(fake.task("agctl", name))["AGCTL_LLM_MODEL"] != "m" {
		t.Fatal("without --ax-recreate-on-drift the task must stay as created")
	}
	cfg.RecreateOnDrift = true
	recreating, _ := NewWithClient(cfg, client, router.Client())
	if _, err := recreating.EnsureChat(ctx, changed); err != nil {
		t.Fatal(err)
	}
	if fake.deletes != 1 || envOf(fake.task("agctl", name))["AGCTL_LLM_MODEL"] != "other-model" {
		t.Fatal("recreate-on-drift must delete and recreate the task with the new spec")
	}
}

func TestFailedTaskAndMissingTaskAreReported(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	provider, _ := NewWithClient(testConfig(router.URL), client, router.Client())
	ctx := context.Background()
	handle, err := provider.EnsureChat(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	name := TaskName("chat-abcd1234-u17")
	fake.mu.Lock()
	fake.tasks[key("agctl", name)].Status.Phase = v1alpha1.PhaseFailed
	fake.tasks[key("agctl", name)].Status.Conditions = []*v1alpha1.Condition{{Type: v1alpha1.ConditionReady, Status: "False", Reason: "ActorResumeFailed", Message: "no free workers"}}
	fake.mu.Unlock()
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "unhealthy" {
		t.Fatalf("failed task health = %q", st)
	}
	if _, err := provider.EnsureChat(ctx, spec()); err == nil || !strings.Contains(err.Error(), "no free workers") {
		t.Fatalf("failed task must surface the reason: %v", err)
	}
	if err := provider.DeleteTask(ctx, name); err != nil {
		t.Fatal(err)
	}
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "unhealthy" {
		t.Fatal("missing task must be unhealthy so the gateway recreates it")
	}
	if err := provider.StopChat(ctx, handle); err != nil {
		t.Fatalf("stopping a missing task must be a no-op: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	_, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.Image = "reg.invalid/agctl-dsh:latest"
	if _, err := NewWithClient(cfg, client, nil); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("tag-only image must be rejected: %v", err)
	}
	cfg = testConfig("not a url")
	if _, err := NewWithClient(cfg, client, nil); err == nil {
		t.Fatal("invalid router URL must be rejected")
	}
	cfg = testConfig(router.URL)
	cfg.RunnerToken = ""
	provider, _ := NewWithClient(cfg, client, router.Client())
	if _, err := provider.LogsChat(context.Background(), agctlrt.ChatHandle{RuntimeID: "x"}, "10"); err == nil {
		t.Fatal("boot log without runner token must fail explicitly")
	}
}

func TestTaskNameIsRFC1123(t *testing.T) {
	if got := TaskName("chat-ABCD1234-u17"); got != "agctl-chat-abcd1234-u17" {
		t.Fatalf("TaskName = %q", got)
	}
	long := TaskName(strings.Repeat("x", 100) + "_y")
	if len(long) > 63 || strings.Contains(long, "_") {
		t.Fatalf("long names must be shortened and sanitized: %q (%d)", long, len(long))
	}
}

func TestDebugSSHPropagatesToTaskSpec(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.DebugSSH = true
	provider, _ := NewWithClient(cfg, client, router.Client())
	dev := spec()
	dev.SSHPubKey = "ssh-ed25519 AAAA dev@test"
	if _, err := provider.EnsureChat(context.Background(), dev); err != nil {
		t.Fatal(err)
	}
	if !fake.task("agctl", TaskName("chat-abcd1234-u17")).GetSpec().GetDebug() {
		t.Fatal("--ax-debug-ssh + SSH key must set spec.debug so ax ssh can reach the sandbox")
	}
}

func TestDebugSSHOffByDefault(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	provider, _ := NewWithClient(testConfig(router.URL), client, router.Client())
	if _, err := provider.EnsureChat(context.Background(), spec()); err != nil {
		t.Fatal(err)
	}
	if fake.task("agctl", TaskName("chat-abcd1234-u17")).GetSpec().GetDebug() {
		t.Fatal("spec.debug must default to off (guest services = arbitrary exec)")
	}
}

func TestDebugSSHOnlyForDeveloperKey(t *testing.T) {
	_, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.DebugSSH = true
	provider, _ := NewWithClient(cfg, client, router.Client())
	ctx := context.Background()
	// Пользователь без ключа (роль user) — guest-сервисов нет.
	if _, err := provider.EnsureChat(ctx, spec()); err != nil {
		t.Fatal(err)
	}
	if fake0 := provider.buildTask("x", spec()); fake0.Spec.Debug {
		t.Fatal("user without SSH key must not get spec.debug")
	}
	// Developer/admin с ключом — есть.
	dev := spec()
	dev.SSHPubKey = "ssh-ed25519 AAAA dev@test"
	if task := provider.buildTask("x", dev); !task.Spec.Debug {
		t.Fatal("developer with SSH key must get spec.debug")
	}
	// Загрузка ключа позже — дрейф specHash (виден пересозданием/предупреждением).
	if specHash(provider.buildTask("x", spec())) == specHash(provider.buildTask("x", dev)) {
		t.Fatal("debug flip must change specHash (late key upload = visible drift)")
	}
	// Выключенный флаг — нет даже с ключом.
	cfgOff := testConfig(router.URL)
	offProvider, _ := NewWithClient(cfgOff, client, router.Client())
	if task := offProvider.buildTask("x", dev); task.Spec.Debug {
		t.Fatal("--ax-debug-ssh=0 must keep spec.debug off")
	}
}
