package dockerrt

// egress.go — opt-in egress-allowlist контейнеров agctl (карточка
// egress-allowlist): контейнеры агента садятся на выделенную сеть
// agctl-egress-<hash дома>, а в DOCKER-USER ставится правило: наружу из
// подсети только DNS (udp/tcp 53); established/related — можно (ответы на
// опубликованные порты), остальное — DROP. Хост-шлюз (LLM на хосте,
// host.docker.internal) не проходит через FORWARD и остаётся доступен.
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
func egressRulesScript(chain, subnet, gateway string) string {
	return strings.Join([]string{
		"set -e",
		fmt.Sprintf("iptables -N %s 2>/dev/null || true", chain),
		fmt.Sprintf("iptables -F %s", chain),
		fmt.Sprintf("iptables -A %s -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -p udp --dport 53 -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -p tcp --dport 53 -j ACCEPT", chain, subnet),
		fmt.Sprintf("iptables -A %s -s %s -d %s -j ACCEPT", chain, subnet, gateway),
		fmt.Sprintf("iptables -A %s -s %s -j DROP", chain, subnet),
		fmt.Sprintf("iptables -C DOCKER-USER -j %s 2>/dev/null || iptables -I DOCKER-USER 1 -j %s", chain, chain),
	}, "\n")
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
func (rt *Runtime) applyEgressRules(ctx context.Context, chain, subnet, gateway string) error {
	hctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := rt.cli.ContainerCreate(hctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      rt.cfg.Image,
			Entrypoint: []string{"sh", "-c", egressRulesScript(chain, subnet, gateway)},
			Labels:     map[string]string{labelWorkspace: "egress-helper"},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "host",
			CapAdd:      []string{"NET_ADMIN"},
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
