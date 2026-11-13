package axrt

// egress_test.go — авто-egress-инъекция ключей LLM: Secret'ы, правила
// EgressPolicy (replaceHeaders Authorization → ate-secret URI), placeholder
// в env задачи, идемпотентность и ротация ключа.

import (
	"context"
	"fmt"
	"testing"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// fakeControl — Control API Substrate в памяти: политика "default" на актора.
type fakeControl struct {
	policies map[string]*ateapi.EgressPolicy
	creates  int
	updates  int
	lastReq  *ateapi.UpdateActorEgressPolicyRequest
}

func newFakeControl() *fakeControl { return &fakeControl{policies: map[string]*ateapi.EgressPolicy{}} }

func actorKey(actor *ateapi.ObjectRef) string { return actor.GetAtespace() + "/" + actor.GetName() }

func (f *fakeControl) GetActorEgressPolicy(_ context.Context, req *ateapi.GetActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapi.EgressPolicy, error) {
	if p, ok := f.policies[actorKey(req.Actor)]; ok {
		return proto.Clone(p).(*ateapi.EgressPolicy), nil
	}
	return nil, status.Errorf(codes.NotFound, "no policy for %s", req.Actor.GetName())
}

func (f *fakeControl) CreateActorEgressPolicy(_ context.Context, req *ateapi.CreateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapi.EgressPolicy, error) {
	k := actorKey(req.Actor)
	if _, exists := f.policies[k]; exists {
		return nil, status.Error(codes.AlreadyExists, "policy exists")
	}
	p := proto.Clone(req.EgressPolicy).(*ateapi.EgressPolicy)
	p.Metadata.Uid = fmt.Sprintf("uid-%s", req.Actor.GetName())
	p.Metadata.Version = 1
	f.policies[k] = p
	f.creates++
	return proto.Clone(p).(*ateapi.EgressPolicy), nil
}

func (f *fakeControl) UpdateActorEgressPolicy(_ context.Context, req *ateapi.UpdateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapi.EgressPolicy, error) {
	k := actorKey(req.Actor)
	p, ok := f.policies[k]
	if !ok {
		return nil, status.Error(codes.NotFound, "no policy")
	}
	if req.EgressPolicy.GetMetadata().GetUid() != p.GetMetadata().GetUid() || req.EgressPolicy.GetMetadata().GetVersion() != p.GetMetadata().GetVersion() {
		return nil, status.Error(codes.FailedPrecondition, "uid/version mismatch")
	}
	np := proto.Clone(req.EgressPolicy).(*ateapi.EgressPolicy)
	np.Metadata.Version = p.GetMetadata().GetVersion() + 1
	f.policies[k] = np
	f.updates++
	f.lastReq = req
	return proto.Clone(np).(*ateapi.EgressPolicy), nil
}

// fakeSecrets — SecretWriter в памяти.
type fakeSecrets struct {
	data map[string]map[string][]byte
	hits int
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{data: map[string]map[string][]byte{}} }

func (f *fakeSecrets) EnsureSecret(_ context.Context, namespace, name string, data map[string][]byte) error {
	f.hits++
	if f.data[name] == nil || string(f.data[name][SecretKeyToken]) != string(data[SecretKeyToken]) {
		cp := map[string][]byte{}
		for k, v := range data {
			cp[k] = append([]byte(nil), v...)
		}
		f.data[name] = cp
	}
	return nil
}

func egressFixture(t *testing.T) (*fakeControl, *fakeSecrets, *EgressAutomation) {
	t.Helper()
	control, secrets := newFakeControl(), newFakeSecrets()
	return control, secrets, &EgressAutomation{Control: control, Secrets: secrets, Cluster: "test-cluster", Namespace: "agctl"}
}

func egressSpec() agctlrt.ChatSpec {
	s := spec()
	s.Provider.Extras = []provision.ExtraProvider{{
		Name: "corp", BaseURL: "https://litellm.corp.invalid/v1", Model: "c", Models: []string{"c"}, APIKey: "corp-key",
	}}
	return s
}

func TestEgressPolicyCreatedWithSecretsAndRules(t *testing.T) {
	_, secrets, eg := egressFixture(t)
	ctx := context.Background()
	if err := eg.EnsureEgressPolicy(ctx, "agctl", "agctl-chat-abcd1234-u17", egressSpec()); err != nil {
		t.Fatal(err)
	}
	// Secret на каждый алиас с ключом.
	primarySecret := secretNameFor(egressSpec().Provider.Alias())
	corpSecret := secretNameFor("corp")
	if string(secrets.data[primarySecret][SecretKeyToken]) != "unit-key" {
		t.Fatalf("primary secret: %+v", secrets.data[primarySecret])
	}
	if string(secrets.data[corpSecret][SecretKeyToken]) != "corp-key" {
		t.Fatalf("corp secret: %+v", secrets.data[corpSecret])
	}
	// Правила: https на хосты провайдеров, Authorization → ate-secret.
	pol := eg.Control.(*fakeControl).policies["agctl/agctl-chat-abcd1234-u17"]
	if pol == nil || len(pol.Rules) != 2 {
		t.Fatalf("policy rules: %+v", pol)
	}
	for _, rule := range pol.Rules {
		h := rule.GetHttps().GetHostnames()
		if len(h) != 1 {
			t.Fatalf("rule hostnames: %v", h)
		}
		var cred string
		switch h[0] {
		case "llm.example.invalid":
			cred = fmt.Sprintf("ate-secret://k8s.io/cluster/test-cluster/agctl/%s/token", primarySecret)
		case "litellm.corp.invalid":
			cred = fmt.Sprintf("ate-secret://k8s.io/cluster/test-cluster/agctl/%s/token", corpSecret)
		default:
			t.Fatalf("unexpected host %q", h[0])
		}
		rh := rule.GetHttps().GetEffects().GetReplaceHeaders()
		if len(rh) != 1 || rh[0].Header != "Authorization" || rh[0].Prefix != "Bearer " || rh[0].CredentialUri != cred {
			t.Fatalf("replaceHeaders: %+v", rh)
		}
	}
}

func TestEgressPolicyIdempotentAndRotatesKey(t *testing.T) {
	control, secrets, eg := egressFixture(t)
	ctx := context.Background()
	if err := eg.EnsureEgressPolicy(ctx, "agctl", "actor-1", egressSpec()); err != nil {
		t.Fatal(err)
	}
	creates, updates := control.creates, control.updates
	if err := eg.EnsureEgressPolicy(ctx, "agctl", "actor-1", egressSpec()); err != nil {
		t.Fatal(err)
	}
	if control.creates != creates || control.updates != updates {
		t.Fatalf("unchanged spec must not write policy: creates %d→%d, updates %d→%d", creates, control.creates, updates, control.updates)
	}

	// Ротация ключа: Secret обновлён, политика не меняется (URI тот же).
	rotated := egressSpec()
	rotated.Provider.APIKey = "rotated-key"
	if err := eg.EnsureEgressPolicy(ctx, "agctl", "actor-1", rotated); err != nil {
		t.Fatal(err)
	}
	primarySecret := secretNameFor(rotated.Provider.Alias())
	if string(secrets.data[primarySecret][SecretKeyToken]) != "rotated-key" {
		t.Fatalf("rotated secret: %+v", secrets.data[primarySecret])
	}

	// Смена провайдера: правило заменилось, update с uid/version-предусловиями.
	changed := egressSpec()
	changed.Provider.BaseURL = "https://llm2.example.invalid/v1"
	if err := eg.EnsureEgressPolicy(ctx, "agctl", "actor-1", changed); err != nil {
		t.Fatal(err)
	}
	if control.updates == 0 || control.lastReq == nil {
		t.Fatal("provider change must update the policy")
	}
	if uid := control.lastReq.EgressPolicy.GetMetadata().GetUid(); uid == "" || uid != control.policies["agctl/actor-1"].GetMetadata().GetUid() {
		t.Fatalf("update must carry uid precondition: %q", uid)
	}
	hosts := map[string]bool{}
	for _, r := range control.policies["agctl/actor-1"].Rules {
		for _, h := range r.GetHttps().GetHostnames() {
			hosts[h] = true
		}
	}
	if !hosts["llm2.example.invalid"] || hosts["llm.example.invalid"] {
		t.Fatalf("rules after change: %v", hosts)
	}
}

func TestEgressHostParsing(t *testing.T) {
	cases := []struct {
		raw, host, scheme string
	}{
		{"", "", ""},
		{"https://LLM.Example.com/v1", "llm.example.com", "https"},
		{"http://gw.corp.internal:8080", "gw.corp.internal", "http"},
		{"https://10.1.2.3/v1", "", ""},
		{"https://example.com.", "example.com", "https"},
	}
	for _, c := range cases {
		host, scheme, err := egressHost(c.raw)
		if err != nil || host != c.host || scheme != c.scheme {
			t.Fatalf("egressHost(%q) = %q,%q,%v; want %q,%q", c.raw, host, scheme, err, c.host, c.scheme)
		}
	}
	if _, _, err := egressHost("://bad"); err == nil {
		t.Fatal("malformed URL must fail")
	}
}

func TestEnsureChatWithEgressSetsPlaceholderAndPolicy(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	control, secrets := newFakeControl(), newFakeSecrets()
	cfg := testConfig(router.URL)
	cfg.InjectLLMKey = false
	cfg.Egress = &EgressAutomation{Control: control, Secrets: secrets, Cluster: "test-cluster", Namespace: "agctl"}
	provider, err := NewWithClient(cfg, client, router.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := provider.EnsureChat(ctx, egressSpec()); err != nil {
		t.Fatal(err)
	}
	name := TaskName("chat-abcd1234-u17")
	env := envOf(fake.task("agctl", name))
	if env["AGCTL_LLM_API_KEY"] != PlaceholderAPIKey {
		t.Fatalf("primary key placeholder: %q", env["AGCTL_LLM_API_KEY"])
	}
	if env["AGCTL_LLM_API_KEY_CORP"] != PlaceholderAPIKey {
		t.Fatalf("extra key placeholder: %q", env["AGCTL_LLM_API_KEY_CORP"])
	}
	for k, v := range env {
		if v == "unit-key" || v == "corp-key" {
			t.Fatalf("real key leaked into env %s", k)
		}
	}
	pol := control.policies["agctl/"+name]
	if pol == nil || len(pol.Rules) != 2 {
		t.Fatalf("policy for task actor: %+v", pol)
	}
	if len(secrets.data) != 2 {
		t.Fatalf("secrets: %+v", secrets.data)
	}
}

func TestEgressNotConfiguredFailsExplicitly(t *testing.T) {
	eg := &EgressAutomation{}
	if err := eg.EnsureEgressPolicy(context.Background(), "agctl", "a", spec()); err == nil {
		t.Fatal("zero-value automation must fail explicitly")
	}
	if err := (*EgressAutomation)(nil).EnsureEgressPolicy(context.Background(), "agctl", "a", spec()); err == nil {
		t.Fatal("nil automation must fail explicitly")
	}
}
