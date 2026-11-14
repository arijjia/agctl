package k8srt

// egress_test.go — юнит-тесты opt-in egress-политики чат-подов (карточка
// k8s-pod-egress-policy): hostname'ы провайдеров, генерация политики,
// сравнение правил, create-or-update на fake clientset.

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEgressHostnames(t *testing.T) {
	spec := agctlrt.ChatSpec{Provider: provision.Provider{
		BaseURL:  "https://api.llm.example/v1/", // path и слэш отрезаются
		BaseURLs: []string{"https://api.llm.example", "https://alt.llm.example:8443/v1"},
		Extras: []provision.ExtraProvider{
			{Name: "pi", BaseURL: "https://pi.example/api"},
			{Name: "dup", BaseURL: "https://api.llm.example/v1"}, // дубль хоста
		},
	}}
	got := egressHostnames(spec)
	want := []string{"alt.llm.example", "api.llm.example", "pi.example"}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("egressHostnames = %v, want %v", got, want)
	}
	for i, h := range want {
		if got[i] != h {
			t.Fatalf("egressHostnames[%d] = %s, want %s (all: %v)", i, got[i], h, got)
		}
	}
	if empty := egressHostnames(agctlrt.ChatSpec{}); len(empty) != 0 {
		t.Fatalf("пустой провайдер дал хосты: %v", empty)
	}
}

func TestHostnameOf(t *testing.T) {
	cases := map[string]string{
		"https://api.llm.example/v1": "api.llm.example",
		"http://llm:8080":            "llm",
		"llm.internal.svc:9000":      "llm.internal.svc",
		"  ":                         "",
	}
	for in, want := range cases {
		if got := hostnameOf(in); got != want {
			t.Fatalf("hostnameOf(%q) = %q, want %q", in, got, want)
		}
	}
	if got := hostnameOf("https://10.1.2.3/v1"); got != "10.1.2.3" {
		t.Fatalf("IP-литерал потерян: %q", got)
	}
}

func TestResolveCIDRs(t *testing.T) {
	// IP-литерал — без lookup'а.
	cidrs, err := resolveCIDRs([]string{"10.244.1.5", "10.244.1.5"}, time.Second)
	if err != nil || len(cidrs) != 1 || cidrs[0] != "10.244.1.5/32" {
		t.Fatalf("IP-литерал: %v %v", cidrs, err)
	}
	// localhost резолвится системно (hosts).
	cidrs, err = resolveCIDRs([]string{"localhost"}, time.Second)
	if err != nil || len(cidrs) == 0 {
		t.Fatalf("localhost: %v %v", cidrs, err)
	}
	// Полный провал — громкая ошибка.
	if _, err := resolveCIDRs([]string{"no-such-host.invalid"}, 2*time.Second); err == nil {
		t.Fatal("полный провал ресолва не вернул ошибку")
	}
}

func TestEgressPolicySpec(t *testing.T) {
	labels := map[string]string{"agctl.io/runtime-key": "r1"}
	pol := egressPolicySpec("r1", labels, []string{"10.0.0.1/32", "10.0.0.2/32"})
	if pol.Name != "netegress-r1" {
		t.Fatalf("имя политики: %s", pol.Name)
	}
	if len(pol.Spec.PolicyTypes) != 1 || pol.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policy types: %v", pol.Spec.PolicyTypes)
	}
	if len(pol.Spec.Egress) != 2 {
		t.Fatalf("правил egress: %d (DNS + ipBlock)", len(pol.Spec.Egress))
	}
	dns := pol.Spec.Egress[0]
	if len(dns.Ports) != 2 { // udp+tcp 53
		t.Fatalf("DNS портов: %d", len(dns.Ports))
	}
	for _, p := range dns.Ports {
		if *p.Protocol != corev1.ProtocolUDP && *p.Protocol != corev1.ProtocolTCP {
			t.Fatalf("протокол DNS: %s", *p.Protocol)
		}
		if p.Port.IntValue() != 53 {
			t.Fatalf("порт DNS: %d", p.Port.IntValue())
		}
	}
	peer := dns.To[0]
	if peer.NamespaceSelector == nil || peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != kubeSystemNS {
		t.Fatalf("kube-dns namespace selector: %+v", peer.NamespaceSelector)
	}
	if peer.PodSelector == nil || peer.PodSelector.MatchLabels["k8s-app"] != kubeDNSApp {
		t.Fatalf("kube-dns pod selector: %+v", peer.PodSelector)
	}
	blocks := pol.Spec.Egress[1]
	if len(blocks.To) != 2 || blocks.To[0].IPBlock.CIDR != "10.0.0.1/32" {
		t.Fatalf("ipBlock: %+v", blocks.To)
	}
	// Пустой список хостов — только DNS (pod без egress к LLM).
	onlyDNS := egressPolicySpec("r2", labels, nil)
	if len(onlyDNS.Spec.Egress) != 1 {
		t.Fatalf("без CIDR должен остаться только DNS: %+v", onlyDNS.Spec.Egress)
	}
}

func TestEqualEgressRules(t *testing.T) {
	a := egressPolicySpec("r", nil, []string{"10.0.0.1/32"}).Spec.Egress
	b := egressPolicySpec("r", nil, []string{"10.0.0.1/32"}).Spec.Egress
	if !equalEgressRules(a, b) {
		t.Fatal("идентичные правила не равны")
	}
	c := egressPolicySpec("r", nil, []string{"10.0.0.9/32"}).Spec.Egress
	if equalEgressRules(a, c) {
		t.Fatal("разные CIDR признаны равными")
	}
	d := egressPolicySpec("r", nil, []string{"10.0.0.1/32", "10.0.0.2/32"}).Spec.Egress
	if equalEgressRules(a, d) {
		t.Fatal("лишний CIDR не замечен")
	}
}

func TestEnsureChatEgressPolicyLifecycle(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "img:1", EgressPolicy: true}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := agctlrt.ChatSpec{
		RuntimeID: "chat-a1b2c3-u5", StorageID: "u5", UserID: 5,
		Provider: provision.Provider{
			// IP-литерал: без DNS-lookup'а тест детерминирован.
			BaseURL: "https://10.66.0.10/v1",
			Extras:  []provision.ExtraProvider{{Name: "pi", BaseURL: "https://10.66.0.11/api"}},
		},
	}
	name := resourceName(spec.RuntimeID)
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	pol, err := client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "netegress-"+name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("egress-политика не создана: %v", err)
	}
	var cidrs []string
	for _, r := range pol.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock != nil {
				cidrs = append(cidrs, peer.IPBlock.CIDR)
			}
		}
	}
	if len(cidrs) != 2 { // оба эндпоинта провайдера
		t.Fatalf("ipBlock CIDR'ов: %v", cidrs)
	}
	// Смена провайдера (новый CIDR) — политика обновляется, не дублируется.
	spec.Provider.BaseURL = "https://10.66.0.99/v1"
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	pol, err = client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "netegress-"+name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range pol.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock != nil && peer.IPBlock.CIDR == "10.66.0.99/32" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("политика не обновилась под новый эндпоинт")
	}
	// StopChat удаляет egress-политику.
	if err := provider.StopChat(ctx, agctlrt.ChatHandle{RuntimeID: spec.RuntimeID}); err != nil {
		t.Fatal(err)
	}
	_, err = client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "netegress-"+name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("egress-политика пережила StopChat: %v", err)
	}
}

func TestEnsureChatEgressResolveFailureIsLoud(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "img:1", EgressPolicy: true}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := agctlrt.ChatSpec{
		RuntimeID: "chat-a1b2c3-u6", StorageID: "u6", UserID: 6,
		Provider: provision.Provider{BaseURL: "https://no-such-host.invalid/v1"},
	}
	if _, err := provider.EnsureChat(ctx, spec); err == nil {
		t.Fatal("полный провал ресолва должен падать громко")
	}
}
