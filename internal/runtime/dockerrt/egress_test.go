package dockerrt

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// TestEgressRulesScript — правила идемпотентны и полны: цепочка
// пересоздаётся (N||true + F), established/DNS разрешены, хвост — DROP,
// jump из DOCKER-USER ставится с проверкой (-C || -I).
func TestEgressRulesScript(t *testing.T) {
	script := egressRulesScript("AGCTL-EGRESS-AB12", "172.18.0.0/16", "172.18.0.1", []string{"10.20.0.0/16", "203.0.113.7"})
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
		`HG="$(getent ahostsv4 agctl-host-gateway 2>/dev/null | awk 'NR==1{print $1}')"`,
		`if [ -n "$HG" ]; then iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -d "$HG" -j ACCEPT; fi`,
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -d 10.20.0.0/16 -j ACCEPT",
		"iptables -A AGCTL-EGRESS-AB12 -s 172.18.0.0/16 -d 203.0.113.7 -j ACCEPT",
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
	allowIdx := strings.Index(script, "-d 203.0.113.7")
	if estIdx > dnsIdx || dnsIdx > dropIdx || allowIdx > dropIdx {
		t.Fatal("порядок правил: established → DNS → белый список → DROP (иначе DROP съест разрешённое)")
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
// правила применяются идемпотентно, из контейнера в сети разрешённый IP
// доступен, неразрешённый — нет, сервис на хосте через host.docker.internal
// (мост к хосту, LiteLLM на хосте) доступен. Без docker/образа — skip.
func TestEgressLive(t *testing.T) {
	image := "agctl-dsh:0.2.2"
	if v := os.Getenv("AGCTL_TEST_IMAGE"); v != "" {
		image = v
	}
	rt, err := New(Config{Image: image, Version: "live-test", Home: t.TempDir(), NoHardening: true}, nil)
	if err != nil {
		t.Skipf("docker недоступен: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	// Egress — iptables на Linux-демоне через хелпер из образа рабочего
	// места: без Linux-демона или без образа проверять нечего.
	info, err := rt.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		t.Skipf("docker info: %v", err)
	}
	if info.Info.OSType != "linux" {
		t.Skipf("демон Docker не Linux (OSType=%q): bridge-сети и iptables нет", info.Info.OSType)
	}
	if _, err := rt.cli.ImageInspect(ctx, rt.cfg.Image); err != nil {
		t.Skipf("нет образа %s (соберите make img_build): %v", rt.cfg.Image, err)
	}

	name, subnet, gateway, err := rt.ensureEgressNetwork(ctx)
	if err != nil {
		t.Fatalf("ensureEgressNetwork: %v", err)
	}
	chain := egressChainName(rt.cfg.Home)
	defer func() {
		_ = rt.runEgressHelper(context.Background(), egressCleanupScript(chain))
		_, _ = rt.cli.NetworkRemove(context.Background(), name, client.NetworkRemoveOptions{})
	}()
	if !strings.HasSuffix(name, egressHomeTag(rt.cfg.Home)) || !strings.Contains(subnet, "/") {
		t.Fatalf("ensureEgressNetwork: name=%s subnet=%s", name, subnet)
	}
	const allowedIP, deniedIP = "1.1.1.1", "9.9.9.9"
	allowed := rt.egressUnion([]string{allowedIP})
	if err := rt.applyEgressRules(ctx, chain, subnet, gateway, allowed); err != nil {
		t.Fatalf("applyEgressRules: %v", err)
	}
	// Идемпотентность: повторное применение тех же правил не падает.
	if err := rt.applyEgressRules(ctx, chain, subnet, gateway, allowed); err != nil {
		t.Fatalf("applyEgressRules (повтор): %v", err)
	}

	// Сервис «на хосте»: слушаем все интерфейсы (на Linux host-gateway —
	// адрес docker0, на Docker Desktop — loopback хоста через VM).
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	hostPort := ln.Addr().(*net.TCPAddr).Port

	probe := func(target string) string {
		return fmt.Sprintf("if timeout 5 bash -c '</dev/tcp/%s' 2>/dev/null; then echo %s=open; else echo %s=closed; fi", target, target, target)
	}
	script := strings.Join([]string{
		probe(allowedIP + "/443"),
		probe(deniedIP + "/443"),
		probe(fmt.Sprintf("host.docker.internal/%d", hostPort)),
	}, "; ")
	out := rt.runProbe(ctx, t, name, script)
	t.Logf("проба из сети egress:\n%s", out)
	for _, want := range []string{
		allowedIP + "/443=open",
		deniedIP + "/443=closed",
		fmt.Sprintf("host.docker.internal/%d=open", hostPort),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ожидали %q, вывод пробы:\n%s", want, out)
		}
	}
}

// runProbe — одноразовый контейнер в сети egress с host.docker.internal,
// как у рабочего места; возвращает его вывод.
func (rt *Runtime) runProbe(ctx context.Context, t *testing.T, netName, script string) string {
	t.Helper()
	res, err := rt.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: rt.cfg.Image, Entrypoint: []string{"bash", "-c", script}},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode(netName),
			ExtraHosts:  []string{"host.docker.internal:host-gateway"},
		},
	})
	if err != nil {
		t.Fatalf("probe create: %v", err)
	}
	defer func() {
		_, _ = rt.cli.ContainerRemove(context.Background(), res.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := rt.cli.ContainerStart(ctx, res.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatalf("probe start: %v", err)
	}
	wait := rt.cli.ContainerWait(ctx, res.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-wait.Result:
	case err := <-wait.Error:
		t.Fatalf("probe wait: %v", err)
	}
	out, _ := rt.Logs(ctx, res.ID, "50")
	return out
}

// TestEgressTargets — IPv4 и CIDR как есть, хосты и URL резолвятся в пины
// host:ip, IPv6 и нерезолвящиеся хосты — в ошибки, дубликаты отброшены.
func TestEgressTargets(t *testing.T) {
	lookup := func(h string) ([]net.IP, error) {
		switch h {
		case "gitlab.example.org":
			return []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("192.0.2.10")}, nil
		case "jira.example.org":
			return []net.IP{net.ParseIP("192.0.2.20")}, nil
		}
		return nil, fmt.Errorf("no such host")
	}
	pins, cidrs, errs := egressTargets([]string{
		"gitlab.example.org", "https://jira.example.org/browse", "10.1.2.3",
		"10.0.0.0/8", "10.1.2.3", "2001:db8::/32", "nope.example.org", " ",
	}, lookup)
	if got, want := strings.Join(pins, ","), "gitlab.example.org:192.0.2.10,jira.example.org:192.0.2.20"; got != want {
		t.Errorf("pins = %q, want %q", got, want)
	}
	if got, want := strings.Join(cidrs, ","), "10.1.2.3,10.0.0.0/8"; got != want {
		t.Errorf("cidrs = %q, want %q", got, want)
	}
	if len(errs) != 2 {
		t.Errorf("ошибок %d (IPv6 + нерезолв), ждали 2: %v", len(errs), errs)
	}
	if got, want := strings.Join(pinIPs(append(pins, "v6.example.org:2001:db8::5")), ","), "192.0.2.10,192.0.2.20"; got != want {
		t.Errorf("pinIPs = %q, want %q (IPv6 в IPv4-правила не идёт)", got, want)
	}
}

// TestEgressUnion — объединение растёт и не теряет адреса прежних стартов.
func TestEgressUnion(t *testing.T) {
	rt := &Runtime{}
	rt.egressUnion([]string{"192.0.2.1"})
	got := rt.egressUnion([]string{"192.0.2.2", "192.0.2.1"})
	if strings.Join(got, ",") != "192.0.2.1,192.0.2.2" {
		t.Fatalf("union = %v", got)
	}
}
