package main

// runtime_select.go — выбор бэкенда персональных рантаймов для `agctl serve`:
// docker (по умолчанию, рабочая станция/один хост), k8s (под на пользователя в
// кластере, internal/runtime/k8srt) или ax (Google AX поверх Agent Substrate:
// задача-актор на пользователя в gVisor/microVM-песочнице, internal/runtime/
// axrt). Флаги/окружение/config.yaml — как у остальных настроек serve.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/runtime/axrt"
	"github.com/arijjia/agctl/internal/runtime/k8srt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
	corev1 "k8s.io/api/core/v1"
)

const (
	runtimeDocker = "docker"
	runtimeK8s    = "k8s"
	runtimeAX     = "ax"
)

// parseRuntimeKind — нормализованное имя бэкенда или ошибка.
func parseRuntimeKind(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", runtimeDocker:
		return runtimeDocker, nil
	case runtimeK8s, "kubernetes", "kube":
		return runtimeK8s, nil
	case runtimeAX, "substrate", "ate":
		return runtimeAX, nil
	}
	return "", fmt.Errorf("--runtime %q: ожидается docker, k8s или ax", raw)
}

// runtimeFlags — флаги бэкендов (регистрируются только в serve).
type runtimeFlags struct {
	kind *string

	k8sNamespace     *string
	k8sKubeconfig    *string
	k8sImage         *string
	k8sStorageClass  *string
	k8sStorageSize   *string
	k8sRuntimeClass  *string
	k8sSharedClaim   *string
	k8sSSHHost       *string
	k8sReadyTimeout  *int
	k8sGatewayLabels *string
	k8sLiveness      *bool
	k8sPullPolicy    *string

	axServer          *string
	axAtespace        *string
	axImage           *string
	axRouterURL       *string
	axRouterCA        *string
	axRouterCert      *string
	axRouterKey       *string
	axReadyTimeout    *int
	axInjectLLMKey    *bool
	axRecreateOnDrift *bool
	axAteAPI          *string
	axAteCluster      *string
}

// registerRuntimeFlags — флаги с дефолтами из env (AGCTL_RUNTIME, AGCTL_K8S_*,
// AGCTL_AX_*) и config.yaml (serve.runtime, serve.k8s.*, serve.ax.*).
func registerRuntimeFlags(fsSet *flag.FlagSet, cfg userConfig) *runtimeFlags {
	rf := &runtimeFlags{}
	rf.kind = fsSet.String("runtime", envOr("AGCTL_RUNTIME", firstNonEmpty(cfg.Serve.Runtime, runtimeDocker)), "бэкенд персональных рантаймов: docker | k8s | ax (Google AX + Agent Substrate)")

	k := cfg.Serve.K8s
	rf.k8sNamespace = fsSet.String("k8s-namespace", envOr("AGCTL_K8S_NAMESPACE", k.Namespace), "namespace подов пользователей (пусто = namespace шлюза в кластере)")
	rf.k8sKubeconfig = fsSet.String("k8s-kubeconfig", envOr("AGCTL_K8S_KUBECONFIG", k.Kubeconfig), "kubeconfig (пусто = in-cluster service account)")
	rf.k8sImage = fsSet.String("k8s-image", envOr("AGCTL_K8S_IMAGE", k.Image), "образ agctl-dsh в реестре, доступном кластеру (пусто = --image)")
	rf.k8sStorageClass = fsSet.String("k8s-storage-class", envOr("AGCTL_K8S_STORAGE_CLASS", k.StorageClass), "StorageClass PVC пользователей (пусто = дефолт кластера)")
	rf.k8sStorageSize = fsSet.String("k8s-storage-size", envOr("AGCTL_K8S_STORAGE_SIZE", firstNonEmpty(k.StorageSize, "5Gi")), "размер PVC пользователя")
	rf.k8sRuntimeClass = fsSet.String("k8s-runtime-class", envOr("AGCTL_K8S_RUNTIME_CLASS", k.RuntimeClass), "RuntimeClass подов (например gvisor)")
	rf.k8sSharedClaim = fsSet.String("k8s-shared-claim", envOr("AGCTL_K8S_SHARED_CLAIM", k.SharedClaim), "имя общего RWX PVC (данные пользователей в <StorageID>/; шлюз монтирует его как $AGCTL_HOME/chats); пусто = PVC на пользователя")
	rf.k8sSSHHost = fsSet.String("k8s-ssh-host", envOr("AGCTL_K8S_SSH_HOST", k.SSHHost), "внешний адрес узлов/балансировщика для SSH developer'а (NodePort); пусто = SSH выключен")
	rf.k8sReadyTimeout = fsSet.Int("k8s-ready-timeout", nonZeroOr(envInt("AGCTL_K8S_READY_TIMEOUT", 0), nonZeroOr(k.ReadyTimeoutSec, 180)), "секунд ожидания готовности пода при старте рантайма")
	rf.k8sGatewayLabels = fsSet.String("k8s-gateway-selector", envOr("AGCTL_K8S_GATEWAY_SELECTOR", firstNonEmpty(k.GatewaySelector, "app.kubernetes.io/name=agctl")), "метки подов шлюза (k=v,…) для NetworkPolicy ingress на dsh; пусто = любой под namespace")
	rf.k8sLiveness = fsSet.Bool("k8s-liveness", envOr("AGCTL_K8S_LIVENESS", "1") != "0", "livenessProbe dsh (agctl-dsh-net health)")
	rf.k8sPullPolicy = fsSet.String("k8s-pull-policy", envOr("AGCTL_K8S_PULL_POLICY", firstNonEmpty(k.PullPolicy, "IfNotPresent")), "imagePullPolicy подов: IfNotPresent | Always | Never")

	a := cfg.Serve.AX
	rf.axServer = fsSet.String("ax-server", envOr("AGCTL_AX_SERVER", firstNonEmpty(a.Server, "ax-server.ax-system.svc.cluster.local:8080")), "gRPC-адрес ax-server")
	rf.axAtespace = fsSet.String("ax-atespace", envOr("AGCTL_AX_ATESPACE", firstNonEmpty(a.Atespace, "agctl")), "atespace задач пользователей (одна установка = один atespace)")
	rf.axImage = fsSet.String("ax-image", envOr("AGCTL_AX_IMAGE", a.Image), "образ песочницы agctl-dsh, pinned by digest (name@sha256:…)")
	rf.axRouterURL = fsSet.String("ax-router-url", envOr("AGCTL_AX_ROUTER_URL", firstNonEmpty(a.RouterURL, "http://atenet-router.ate-system.svc.cluster.local:80")), "URL маршрутизатора Substrate (atenet-router)")
	rf.axRouterCA = fsSet.String("ax-router-ca", envOr("AGCTL_AX_ROUTER_CA", a.RouterCA), "PEM CA маршрутизатора (https)")
	rf.axRouterCert = fsSet.String("ax-router-cert", envOr("AGCTL_AX_ROUTER_CERT", a.RouterCert), "PEM клиентского сертификата mTLS маршрутизатора (PodCertificate)")
	rf.axRouterKey = fsSet.String("ax-router-key", envOr("AGCTL_AX_ROUTER_KEY", a.RouterKey), "PEM ключа клиентского сертификата mTLS")
	rf.axReadyTimeout = fsSet.Int("ax-ready-timeout", nonZeroOr(envInt("AGCTL_AX_READY_TIMEOUT", 0), nonZeroOr(a.ReadyTimeoutSec, 180)), "секунд ожидания Ready задачи после resume")
	rf.axInjectLLMKey = fsSet.Bool("ax-inject-llm-key", envOr("AGCTL_AX_INJECT_LLM_KEY", "1") != "0", "класть ключи LLM в env задачи; 0 = ключи инжектит egress-шлюз Substrate (replaceHeaders)")
	rf.axRecreateOnDrift = fsSet.Bool("ax-recreate-on-drift", envOr("AGCTL_AX_RECREATE_ON_DRIFT", "0") == "1", "пересоздавать задачу при смене образа/провайдера (данные актора теряются)")
	rf.axAteAPI = fsSet.String("ax-ate-api", envOr("AGCTL_AX_ATE_API", a.AteAPI), "адрес ate-api-server (Control API Substrate) для авто-egress-инъекции ключей LLM при --ax-inject-llm-key=0; пусто = политику ведёт оператор")
	rf.axAteCluster = fsSet.String("ax-ate-cluster", envOr("AGCTL_AX_ATE_CLUSTER", firstNonEmpty(a.AteCluster, "cluster.local")), "имя кластера в credentialUri ate-secret://k8s.io/cluster/<cluster>/…")
	return rf
}

// buildServer — сервер с выбранным бэкендом. Docker: server.New (свип дрейфа
// внутри); k8s/ax: NewWithRuntimeProviders с адаптером и как ChatProvider, и
// как legacy RuntimeIface (админские списки/логи).
func buildServer(rf *runtimeFlags, db *store.DB, sec *secrets.Manager, cfg server.Config) (*server.Server, error) {
	kind, err := parseRuntimeKind(*rf.kind)
	if err != nil {
		return nil, err
	}
	switch kind {
	case runtimeDocker:
		return server.New(db, sec, cfg)
	case runtimeK8s:
		k8sCfg := k8srt.Config{
			Namespace:        *rf.k8sNamespace,
			Kubeconfig:       *rf.k8sKubeconfig,
			Image:            firstNonEmpty(*rf.k8sImage, cfg.Image),
			ImagePullPolicy:  corev1.PullPolicy(*rf.k8sPullPolicy),
			StorageClassName: *rf.k8sStorageClass,
			StorageSize:      *rf.k8sStorageSize,
			RuntimeClassName: *rf.k8sRuntimeClass,
			CPULimit:         strconv.FormatFloat(float64(cfg.NanoCPUs)/1e9, 'f', -1, 64),
			MemoryLimit:      strconv.FormatInt(cfg.MemoryBytes>>20, 10) + "Mi",
			SharedClaim:      *rf.k8sSharedClaim,
			Version:          cfg.Version,
			WaitReady:        true,
			ReadyTimeout:     time.Duration(*rf.k8sReadyTimeout) * time.Second,
			SSHHost:          *rf.k8sSSHHost,
			GatewaySelector:  parseLabels(*rf.k8sGatewayLabels),
			Liveness:         *rf.k8sLiveness,
		}
		if cfg.NanoCPUs <= 0 {
			k8sCfg.CPULimit = ""
		}
		if cfg.MemoryBytes <= 0 {
			k8sCfg.MemoryLimit = ""
		}
		provider, err := k8srt.New(k8sCfg)
		if err != nil {
			return nil, err
		}
		srv, err := server.NewWithRuntimeProviders(db, sec, cfg, provider, provider)
		if err != nil {
			return nil, err
		}
		sweepCtx, cancelSweep := context.WithTimeout(context.Background(), 30*time.Second)
		n := provider.SweepVersionDrift(sweepCtx)
		cancelSweep()
		if n > 0 {
			log.Printf("[chat] k8s: свип дрейфа — удалены поды прошлой сборки: %d", n)
		}
		log.Printf("runtime: kubernetes namespace=%s image=%s shared-claim=%q ssh-host=%q", provider.Namespace(), k8sCfg.Image, k8sCfg.SharedClaim, k8sCfg.SSHHost)
		return srv, nil
	case runtimeAX:
		token, err := axRunnerToken(db, sec)
		if err != nil {
			return nil, err
		}
		axCfg := axrt.Config{
			Server:           *rf.axServer,
			Atespace:         *rf.axAtespace,
			Image:            *rf.axImage,
			RouterURL:        *rf.axRouterURL,
			RouterCA:         *rf.axRouterCA,
			RouterClientCert: *rf.axRouterCert,
			RouterClientKey:  *rf.axRouterKey,
			RunnerToken:      token,
			Version:          cfg.Version,
			ReadyTimeout:     time.Duration(*rf.axReadyTimeout) * time.Second,
			InjectLLMKey:     *rf.axInjectLLMKey,
			RecreateOnDrift:  *rf.axRecreateOnDrift,
			CPU:              strconv.FormatFloat(float64(cfg.NanoCPUs)/1e9, 'f', -1, 64),
			Memory:           strconv.FormatInt(cfg.MemoryBytes>>20, 10) + "Mi",
		}
		if cfg.NanoCPUs <= 0 {
			axCfg.CPU = ""
		}
		if cfg.MemoryBytes <= 0 {
			axCfg.Memory = ""
		}
		if !axCfg.InjectLLMKey && *rf.axAteAPI != "" {
			eg, err := axEgressAutomation(*rf.axAteAPI, *rf.axAteCluster, *rf.k8sKubeconfig)
			if err != nil {
				return nil, err
			}
			axCfg.Egress = eg
		}
		provider, err := axrt.New(axCfg)
		if err != nil {
			return nil, err
		}
		srv, err := server.NewWithRuntimeProviders(db, sec, cfg, provider, provider)
		if err != nil {
			_ = provider.Close()
			return nil, err
		}
		log.Printf("runtime: ax server=%s atespace=%s image=%s router=%s inject-llm-key=%v", axCfg.Server, axCfg.Atespace, axCfg.Image, axCfg.RouterURL, axCfg.InjectLLMKey)
		return srv, nil
	}
	return nil, errors.New("runtime: unreachable")
}

// parseLabels — "k=v,k2=v2" → map (пустые пары пропускаются).
func parseLabels(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const axRunnerTokenKey = "ax_runner_token"

// axRunnerToken — секрет доступа к /agctl/boot-log раннеров AX: один на
// установку, генерируется при первом старте и хранится зашифрованным в
// SQLite (задача AX неизменяема — токен должен переживать рестарты шлюза).
func axRunnerToken(db *store.DB, sec *secrets.Manager) (string, error) {
	if enc, err := db.SettingGet(axRunnerTokenKey); err == nil && enc != "" {
		token, err := sec.Decrypt(enc)
		if err == nil && token != "" {
			return token, nil
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("ax runner token: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	enc, err := sec.Encrypt(token)
	if err != nil {
		return "", err
	}
	if err := db.SettingSet(axRunnerTokenKey, enc); err != nil {
		return "", fmt.Errorf("ax runner token: %w", err)
	}
	return token, nil
}

// runtimeStatusLine — строка для лога старта вместо docker-подсказки.
func runtimeStatusLine(kind string) string {
	switch kind {
	case runtimeK8s:
		return "kubernetes: поды пользователей через in-cluster API"
	case runtimeAX:
		return "ax: задачи пользователей через ax-server / Agent Substrate"
	}
	return "docker: " + dockerStatus()
}
