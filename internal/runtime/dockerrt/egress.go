package dockerrt

// egress.go — opt-in egress-allowlist контейнеров agctl (карточка
// egress-allowlist): контейнеры агента садятся на выделенную сеть
// agctl-egress-<hash дома>, а в DOCKER-USER ставится правило: наружу из
// подсети только DNS (udp/tcp 53), established/related (ответы на
// опубликованные порты), шлюз сети, адрес host-gateway (host.docker.internal:
// мост к хосту, LiteLLM на хосте) и белый список — IP провайдеров модели и
// EgressAllow (карточка dockerrt-egress-flag-key-proxy); остальное — DROP.
//
// host-gateway в Docker Desktop — не шлюз сети, а 192.168.65.254 (и IPv6,
// недостижимый из IPv4-сети): хелпер узнаёт его IPv4 через --add-host
// agctl-host-gateway:host-gateway и разрешает отдельным правилом.
//
// Правила применяются хелпер-контейнером `--network host --cap-add NET_ADMIN`
// из того же образа: это работает и на Linux-хосте (host = netns хоста
// демона), и в Docker Desktop (host = netns VM, где живёт DOCKER-USER).
// Поэтому в образ обязан входить iptables (Dockerfile).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	// egressNetworkPrefix — выделенная сеть agctl; одна на дом agctl.
	egressNetworkPrefix = "agctl-egress-"
	// egressChainPrefix — per-home цепочка в DOCKER-USER (несколько agctl на
	// одном демоне не конфликтуют).
	egressChainPrefix = "AGCTL-EGRESS-"
	// egressHostGatewayName — имя host-gateway в /etc/hosts хелпера.
	egressHostGatewayName = "agctl-host-gateway"
)

// egressHomeTag — короткий стабильный тег дома agctl для имён сети/цепочки.
func egressHomeTag(home string) string {
	sum := sha256.Sum256([]byte(home))
	return hex.EncodeToString(sum[:4])
}

// egressNetworkName — имя выделенной сети для дома agctl.
func egressNetworkName(home string) string {
	return egressNetworkPrefix + egressHomeTag(home)
}

// egressChainName — имя iptables-цепочки правил для дома agctl.
func egressChainName(home string) string {
	return egressChainPrefix + strings.ToUpper(egressHomeTag(home))
}

// egressRulesScript — идемпотентный shell-скрипт правил для подсети. Чистая
// функция (тестируется юнит-тестом): цепочка пересоздаётся с нуля, jump из
// DOCKER-USER ставится ровно один раз. Gateway разрешён явно: на Linux
// контейнер→шлюз идёт в INPUT мимо DOCKER-USER, но Docker Desktop форвардит
// такой трафик (vmnet) — без ACCEPT хост-шлюз (LLM на хосте) отвалился бы.
// allowed — IPv4/CIDR белого списка (провайдеры модели, EgressAllow).
func egressRulesScript(chain, subnet, gateway string, allowed []string) string {
	lines := []string{
		"set -e",
		fmt.Sprintf("iptables -N %s 2>/dev/null || true", chain),
		fmt.Sprintf("iptables -F %s", chain),
		fmt.Sprintf("iptables -A %s -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -p udp --dport 53 -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -p tcp --dport 53 -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -d %s -j ACCEPT", chain, subnet, gateway),
		fmt.Sprintf(`HG="$(getent ahostsv4 %s 2>/dev/null | awk 'NR==1{print $1}')"`, egressHostGatewayName),
		fmt.Sprintf(`if [ -n "$HG" ]; then iptables -A %s -s %s -d "$HG" -j ACCEPT; fi`, chain, subnet),
	}
	for _, a := range allowed {
		lines = append(lines, fmt.Sprintf("iptables -A %s -s %s -d %s -j ACCEPT", chain, subnet, a))
	}
	lines = append(lines,
		fmt.Sprintf("iptables -A %s -s %s -j DROP", chain, subnet),
		fmt.Sprintf("iptables -C DOCKER-USER -j %s 2>/dev/null || iptables -I DOCKER-USER 1 -j %s", chain, chain),
	)
	return strings.Join(lines, "\n")
}

// egressTargets — разбор EgressAllow: IPv4 и CIDR идут в правила как есть,
// хосты резолвятся на хосте и пинятся (host:ip для /etc/hosts контейнера —
// контейнер ходит ровно на разрешённый адрес). IPv6 и мусор пропускаются с
// ошибкой для лога: сеть egress — только IPv4.
func egressTargets(entries []string, lookup func(string) ([]net.IP, error)) (pins, cidrs []string, errs []error) {
	seen := map[string]bool{}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		if pfx, err := netip.ParsePrefix(e); err == nil {
			if !pfx.Addr().Is4() {
				errs = append(errs, fmt.Errorf("%s: IPv6 не поддерживается", e))
				continue
			}
			cidrs = append(cidrs, pfx.Masked().String())
			continue
		}
		if ip, err := netip.ParseAddr(e); err == nil {
			if !ip.Is4() {
				errs = append(errs, fmt.Errorf("%s: IPv6 не поддерживается", e))
				continue
			}
			cidrs = append(cidrs, ip.String())
			continue
		}
		host := e
		if u, err := url.Parse(e); err == nil && u.Hostname() != "" && strings.Contains(e, "://") {
			host = u.Hostname()
		}
		pin, err := pinnedHost("https://"+host, lookup)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if pin != "" {
			pins = append(pins, pin)
		}
	}
	return pins, cidrs, errs
}

// pinIPs — IPv4-адреса из пинов host:ip (IPv6 в IPv4-правила не идут).
func pinIPs(pins []string) []string {
	var out []string
	for _, p := range pins {
		i := strings.LastIndex(p, ":")
		if i < 0 {
			continue
		}
		if ip, err := netip.ParseAddr(p[i+1:]); err == nil && ip.Is4() {
			out = append(out, ip.String())
		}
	}
	return out
}

// egressUnion добавляет адреса к уже разрешённым в цепочке дома и
// возвращает отсортированное объединение: цепочка одна на дом, а у
// пользователей разные провайдеры — пересборка не должна отнимать доступ у
// уже работающих контейнеров.
func (rt *Runtime) egressUnion(add []string) []string {
	rt.egressMu.Lock()
	defer rt.egressMu.Unlock()
	if rt.egressAllowed == nil {
		rt.egressAllowed = map[string]bool{}
	}
	for _, a := range add {
		rt.egressAllowed[a] = true
	}
	out := make([]string, 0, len(rt.egressAllowed))
	for a := range rt.egressAllowed {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// ensureEgressNetwork — выделенная bridge-сеть дома; создаётся при первом
// старте, далее переиспользуется. Возвращает имя и подсеть (для правил).
func (rt *Runtime) ensureEgressNetwork(ctx context.Context) (string, string, string, error) {
	name := egressNetworkName(rt.cfg.Home)
	insp, gerr := rt.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if gerr == nil {
		if sub := networkSubnetOf(insp.Network); sub != "" {
			return name, sub, networkGatewayOf(insp.Network), nil
		}
		return "", "", "", fmt.Errorf("docker: сеть %s без подсети (IPAM пуст)", name)
	}
	if !isNotFound(gerr) {
		return "", "", "", fmt.Errorf("docker: inspect сети %s: %w", name, gerr)
	}
	res, err := rt.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{
		Driver: "bridge",
		Labels: map[string]string{labelWorkspace: "egress", "agctl.home": rt.cfg.Home},
	})
	if err != nil {
		return "", "", "", fmt.Errorf("docker: create сети %s: %w", name, err)
	}
	insp2, err := rt.cli.NetworkInspect(ctx, res.ID, client.NetworkInspectOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("docker: inspect созданной сети %s: %w", name, err)
	}
	sub := networkSubnetOf(insp2.Network)
	if sub == "" {
		return "", "", "", fmt.Errorf("docker: созданная сеть %s без подсети", name)
	}
	return name, sub, networkGatewayOf(insp2.Network), nil
}

// networkGatewayOf — gateway IPAM-конфига сети (первая непустая).
func networkGatewayOf(insp network.Inspect) string {
	for _, cfg := range insp.IPAM.Config {
		if cfg.Gateway.IsValid() {
			return cfg.Gateway.String()
		}
	}
	return ""
}

// networkSubnetOf — первая подсеть IPAM сети.
func networkSubnetOf(insp network.Inspect) string {
	for _, cfg := range insp.IPAM.Config {
		if cfg.Subnet.IsValid() {
			return cfg.Subnet.String()
		}
	}
	return ""
}

// applyEgressRules — прогон правил хелпер-контейнером в netns демона
// (--network host + NET_ADMIN): и на Linux-хосте, и в Docker Desktop это
// тот netns, где живёт DOCKER-USER.
func (rt *Runtime) applyEgressRules(ctx context.Context, chain, subnet, gateway string, allowed []string) error {
	return rt.runEgressHelper(ctx, egressRulesScript(chain, subnet, gateway, allowed))
}

// egressCleanupScript — снять jump из DOCKER-USER и удалить цепочку дома
// (тесты; ручная уборка после выключения egress-allowlist).
func egressCleanupScript(chain string) string {
	return strings.Join([]string{
		fmt.Sprintf("while iptables -D DOCKER-USER -j %s 2>/dev/null; do :; done", chain),
		fmt.Sprintf("iptables -F %s 2>/dev/null || true", chain),
		fmt.Sprintf("iptables -X %s 2>/dev/null || true", chain),
	}, "\n")
}

// runEgressHelper выполняет shell-скрипт iptables в netns демона.
func (rt *Runtime) runEgressHelper(ctx context.Context, script string) error {
	hctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := rt.cli.ContainerCreate(hctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      rt.cfg.Image,
			Entrypoint: []string{"sh", "-c", script},
			Labels:     map[string]string{labelWorkspace: "egress-helper"},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "host",
			CapAdd:      []string{"NET_ADMIN"},
			ExtraHosts:  []string{egressHostGatewayName + ":host-gateway"},
			AutoRemove:  false,
		},
	})
	if err != nil {
		return fmt.Errorf("docker: egress-хелпер create: %w", err)
	}
	defer func() { _, _ = rt.cli.ContainerRemove(ctx, res.ID, client.ContainerRemoveOptions{Force: true}) }()
	if _, err := rt.cli.ContainerStart(hctx, res.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("docker: egress-хелпер start: %w", err)
	}
	wait := rt.cli.ContainerWait(hctx, res.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case wr := <-wait.Result:
		if wr.StatusCode != 0 {
			logs, _ := rt.Logs(hctx, res.ID, "20")
			return fmt.Errorf("docker: egress-хелпер exit %d: %s", wr.StatusCode, logs)
		}
	case err := <-wait.Error:
		if err != nil {
			return fmt.Errorf("docker: egress-хелпер: %w", err)
		}
	case <-hctx.Done():
		return fmt.Errorf("docker: egress-хелпер: таймаут")
	}
	return nil
}
