package k8srt

// egress.go — opt-in egress-политика чат-пода (карточка k8s-pod-egress-
// policy): NetworkPolicy Egress с DNS к kube-dns и ipBlock-CIDR'ами
// разрешённых LLM-хостов. Hostname'ы провайдеров берутся из ChatSpec
// (AllEndpoints: базовый провайдер + Extras), шлюз DNS-resolve'ит их при
// EnsureChat и периодически обновляет политику. Ограничение v1: ipBlock
// видит только IP — динамические адреса LLM-провайдеров покрываются
// периодическим ресолвом; L7/FQDN-фильтрация (Cilium) — отдельная волна.
// Требует CNI с поддержкой NetworkPolicy (kindnet не энфорсит политики).

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// egressRefreshInterval — период периодического ресолва hostname'ов
	// провайдеров и обновления политик живых чатов.
	egressRefreshInterval = 10 * time.Minute
	// egressResolveTimeout — таймаут одного DNS-lookup'а hostname'а.
	egressResolveTimeout = 5 * time.Second
	// egressPolicyPrefix — имя egress-политики чата: netegress-<runtime>.
	egressPolicyPrefix = "netegress-"
	// kubeSystemNS / kubeDNSApp — kube-dns (CoreDNS) кластера.
	kubeSystemNS = "kube-system"
	kubeDNSApp   = "kube-dns"
)

// egressHostnames — hostnames всех LLM-эндпоинтов чата: AllEndpoints
// покрывает базовый провайдер (все BaseURL) и Extras[].BaseURL. Пусто =
// провайдер не задан (dsh спросит пользователя — разрешать нечего, pod
// останется без egress: осознанная семантика для install без дефолта).
func egressHostnames(spec agctlrt.ChatSpec) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		h := hostnameOf(raw)
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		out = append(out, h)
	}
	for _, u := range spec.Provider.AllProviderURLs() {
		add(u)
	}
	return out
}

// hostnameOf — hostname из URL (scheme://host:port/path → host); ClusterIP/
// IP-литералы пропускаются как есть (ipBlock примет их напрямую).
func hostnameOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	host := ""
	if u, err := url.Parse(raw); err == nil {
		host = u.Hostname()
	}
	return host
}

// resolveCIDRs — /32 CIDR'ы каждого hostname (номера портов не влияют:
// ipBlock фильтрует по адресам). Ошибки ресолва собираются поимённо; при
// полном провале возвращается ошибка — чат без egress до LLM всё равно
// неработоспособен, громкий фейл лучше тихого.
func resolveCIDRs(hosts []string, timeout time.Duration) ([]string, error) {
	var mu sync.Mutex
	var cidrs []string
	var failed []string
	var wg sync.WaitGroup
	for _, h := range hosts {
		if net.ParseIP(h) != nil {
			mu.Lock()
			cidrs = append(cidrs, h+"/32")
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || len(addrs) == 0 {
				failed = append(failed, host)
				return
			}
			for _, a := range addrs {
				cidrs = append(cidrs, a.IP.String()+"/32")
			}
		}(h)
	}
	wg.Wait()
	if len(failed) > 0 {
		sort.Strings(failed)
		return dedup(cidrs), fmt.Errorf("k8srt: egress-ресолв не удался для %s", strings.Join(failed, ", "))
	}
	return dedup(cidrs), nil
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// egressPolicySpec — NetworkPolicy Egress чат-пода: DNS (udp+tcp 53) к
// kube-dns и ipBlock разрешённых CIDR'ов; весь прочий egress запрещён.
func egressPolicySpec(name string, labels map[string]string, cidrs []string) *networkingv1.NetworkPolicy {
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dns := intstr.FromInt(53)
	dnsPeer := networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": kubeSystemNS}},
		PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": kubeDNSApp}},
	}
	egress := []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{dnsPeer}, Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &udp, Port: &dns},
			{Protocol: &tcp, Port: &dns},
		}},
	}
	if len(cidrs) > 0 {
		blocks := make([]networkingv1.NetworkPolicyPeer, 0, len(cidrs))
		for _, c := range cidrs {
			blocks = append(blocks, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: c}})
		}
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{To: blocks})
	}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: egressPolicyPrefix + name, Labels: labels},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabels(labels)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      egress,
		},
	}
}

// ensureEgressPolicy создаёт/обновляет egress-политику чата (create-or-update
// по значимости правил — как ingress-политика).
func (p *Provider) ensureEgressPolicy(ctx context.Context, name string, labels map[string]string, cidrs []string) error {
	api := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace)
	desired := egressPolicySpec(name, labels, cidrs)
	desired.Namespace = p.cfg.Namespace
	existing, err := api.Get(ctx, desired.Name, metav1.GetOptions{})
	if err == nil {
		if equalEgressRules(existing.Spec.Egress, desired.Spec.Egress) {
			return nil
		}
		existing.Spec.Egress = desired.Spec.Egress
		if _, err := api.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("k8srt: update egress NetworkPolicy %s: %w", desired.Name, err)
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: get egress NetworkPolicy %s: %w", desired.Name, err)
	}
	if _, err := api.Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create egress NetworkPolicy %s: %w", desired.Name, err)
	}
	return nil
}

// equalEgressRules — сравнение правил egress (CIDR-набор может приходить в
// другом порядке после ресолва).
func equalEgressRules(a, b []networkingv1.NetworkPolicyEgressRule) bool {
	key := func(rules []networkingv1.NetworkPolicyEgressRule) []string {
		out := make([]string, 0, len(rules))
		for _, r := range rules {
			parts := make([]string, 0, 4)
			for _, p := range r.Ports {
				parts = append(parts, string(*p.Protocol)+"/"+p.Port.String())
			}
			sort.Strings(parts)
			for _, peer := range r.To {
				switch {
				case peer.IPBlock != nil:
					parts = append(parts, "ip:"+peer.IPBlock.CIDR)
				case peer.PodSelector != nil:
					parts = append(parts, "pod:"+fmt.Sprint(peer.PodSelector.MatchLabels))
				case peer.NamespaceSelector != nil:
					parts = append(parts, "ns:"+fmt.Sprint(peer.NamespaceSelector.MatchLabels))
				}
			}
			sort.Strings(parts)
			out = append(out, strings.Join(parts, ";"))
		}
		sort.Strings(out)
		return out
	}
	ka, kb := key(a), key(b)
	if len(ka) != len(kb) {
		return false
	}
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}

// egressWatch — состояние живых чатов с egress-политикой: runtime name →
// hostname'ы провайдеров (обновляются фоновым рефрешером).
type egressWatch struct {
	mu     sync.Mutex
	hosts  map[string][]string // runtime name → hostnames
	once   sync.Once
	cancel context.CancelFunc
}

// registerEgress — запомнить hostname'ы чата и поднять фоновый рефрешер
// (один на провайдер).
func (p *Provider) registerEgress(name string, hosts []string) {
	p.egress.mu.Lock()
	if p.egress.hosts == nil {
		p.egress.hosts = map[string][]string{}
	}
	p.egress.hosts[name] = hosts
	p.egress.mu.Unlock()
	p.egress.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		p.egress.cancel = cancel
		go p.egressRefreshLoop(ctx)
	})
}

// unregisterEgress — чат уснул: снять с рефрешера и удалить политику.
func (p *Provider) unregisterEgress(ctx context.Context, name string) {
	p.egress.mu.Lock()
	delete(p.egress.hosts, name)
	p.egress.mu.Unlock()
	if err := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace).Delete(ctx, egressPolicyPrefix+name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		log.Printf("k8srt: delete egress NetworkPolicy %s: %v", egressPolicyPrefix+name, err)
	}
}

// egressRefreshLoop — периодический ресолв hostname'ов провайдеров живых
// чатов и обновление их политик (динамические IP LLM-хостов).
func (p *Provider) egressRefreshLoop(ctx context.Context) {
	t := time.NewTicker(egressRefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.egress.mu.Lock()
			pending := make(map[string][]string, len(p.egress.hosts))
			for name, hosts := range p.egress.hosts {
				pending[name] = hosts
			}
			p.egress.mu.Unlock()
			for name, hosts := range pending {
				cidrs, err := resolveCIDRs(hosts, egressResolveTimeout)
				if err != nil {
					// Держим прошлый CIDR-набор: транзиентный DNS не должен
					// закрыть чату ход к LLM.
					log.Printf("k8srt: egress-ресолв %s: %v (политика не менялась)", name, err)
					continue
				}
				if err := p.updateEgressCIDRs(ctx, name, cidrs); err != nil {
					log.Printf("k8srt: egress-политика %s: %v", name, err)
				}
			}
		}
	}
}

// updateEgressCIDRs — обновить egress-политику чата свежим CIDR-набором.
func (p *Provider) updateEgressCIDRs(ctx context.Context, name string, cidrs []string) error {
	api := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace)
	policyName := egressPolicyPrefix + name
	existing, err := api.Get(ctx, policyName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil // чат уснул между снапшотом и обновлением
	} else if err != nil {
		return fmt.Errorf("k8srt: get egress NetworkPolicy %s: %w", policyName, err)
	}
	desired := egressPolicySpec(name, existing.Labels, cidrs)
	if equalEgressRules(existing.Spec.Egress, desired.Spec.Egress) {
		return nil
	}
	existing.Spec.Egress = desired.Spec.Egress
	if _, err := api.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("k8srt: update egress NetworkPolicy %s: %w", policyName, err)
	}
	return nil
}
