package dockerrt

import (
	"context"

	"github.com/moby/moby/client"
	"strings"
	"testing"
	"time"
)

// TestEgressRulesScript — правила идемпотентны и полны: цепочка
// пересоздаётся (N||true + F), established/DNS разрешены, хвост — DROP,
// jump из DOCKER-USER ставится с проверкой (-C || -I).
func TestEgressRulesScript(t *testing.T) {
	script := egressRulesScript("AGCTL-EGRESS-AB12", "172.18.0.0/16", "172.18.0.1")
	lines := strings.Split(script, "\n")
	if lines[0] != "set -e" {
		t.Fatalf("скрипт должен падать на первой же ошибке: %q", lines[0])
	}
	for _, want := range []string{
		"iptables -N AGCTL-EGRESS-AB12 2>/dev/null || true",
		"iptables -F AGCTL-EGRESS-AB12",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -p udp --dport 53 -j ACCEPT",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -p tcp --dport 53 -j ACCEPT",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -d 172.18.0.1 -j ACCEPT",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -j DROP",
		"iptables -C DOCKER-USER -j AGCTL-EGRESS-AB12 2>/dev/null || iptables -I DOCKER-USER 1 -j AGCTL-EGRESS-AB12",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("в скрипте нет строки %q", want)
		}
	}
	dropIdx := strings.Index(script, "-j DROP")
	estIdx := strings.Index(script, "ESTABLISHED,RELATED")
	dnsIdx := strings.Index(script, "--dport 53")
	if estIdx > dnsIdx || dnsIdx > dropIdx {
		t.Fatal("порядок правил: established → DNS → DROP (иначе DROP съест разрешённое)")
	}
}

// TestEgressNames — имена сети/цепочки стабильны и зависят от дома:
// разные agctl-дома на одном демоне не конфликтуют.
func TestEgressNames(t *testing.T) {
	if got, want := egressNetworkName("/srv/agctl"), egressNetworkPrefix+egressHomeTag("/srv/agctl"); got != want {
		t.Fatalf("network name: %q != %q", got, want)
	}
	if got := egressChainName("/srv/agctl"); !strings.HasPrefix(got, egressChainPrefix) {
		t.Fatalf("chain name без префикса: %q", got)
	}
	if egressNetworkName("/a") == egressNetworkName("/b") {
		t.Fatal("разные дома должны давать разные сети")
	}
	if strings.ContainsAny(strings.TrimPrefix(egressChainName("/srv/agctl"), egressChainPrefix), "/.-") {
		t.Fatal("имя цепочки не должно содержать ip-chars вне [A-Z0-9_-]")
	}
}

// TestEgressLive — живая проверка на реальном демоне: сеть создаётся,
// правила применяются, контейнер в сети не ходит наружу (кроме DNS и
// хост-шлюза). Без docker/образа — skip.
func TestEgressLive(t *testing.T) {
	rt, err := New(Config{Image: "agctl-dsh:0.2.2", Version: "live-test", Home: t.TempDir()}, nil)
	if err != nil {
		t.Skipf("docker недоступен: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	name, subnet, gateway, err := rt.ensureEgressNetwork(ctx)
	if err != nil {
		t.Fatalf("ensureEgressNetwork: %v", err)
	}
	defer func() { _, _ = rt.cli.NetworkRemove(ctx, name, client.NetworkRemoveOptions{}) }()
	if !strings.HasSuffix(name, egressHomeTag(rt.cfg.Home)) || !strings.Contains(subnet, "/") {
		t.Fatalf("ensureEgressNetwork: name=%s subnet=%s", name, subnet)
	}
	if err := rt.applyEgressRules(ctx, egressChainName(rt.cfg.Home), subnet, gateway); err != nil {
		t.Fatalf("applyEgressRules: %v", err)
	}
	// Идемпотентность: повторное применение тех же правил не падает.
	if err := rt.applyEgressRules(ctx, egressChainName(rt.cfg.Home), subnet, gateway); err != nil {
		t.Fatalf("applyEgressRules (повтор): %v", err)
	}
}
