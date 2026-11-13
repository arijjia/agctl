// Package k8srt — Kubernetes-адаптер контракта runtime.ChatProvider: один под
// dsh на пользователя (web :3080, для developer'а — sshd :2222), ClusterIP-
// сервис для шлюза, NetworkPolicy, Secret с ключами провайдеров и
// персистентные данные пользователя: либо PVC на пользователя, либо общий
// RWX-том (SharedClaim), который шлюз agctl монтирует у себя как
// $AGCTL_HOME/chats — тогда проектные API шлюза видят файлы пользователя так
// же, как в Docker. Compute удаляется по простою, данные — никогда.
package k8srt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	webPort = 3080
	sshPort = 2222

	labelName       = "app.kubernetes.io/name"
	labelManagedBy  = "app.kubernetes.io/managed-by"
	labelRuntimeKey = "agctl.io/runtime-key"
	labelStorageKey = "agctl.io/storage-key"
	labelRuntimeID  = "agctl.io/runtime-id"
	labelUserID     = "agctl.io/user-id"
	labelVersion    = "agctl.io/version"
	annotationImage = "agctl.io/image"
	appName         = "agctl-dsh-chat"

	defaultReadyTimeout = 180 * time.Second
	readyPollInterval   = 2 * time.Second
)

// Config — настройки адаптера. Namespace — граница хранения: один namespace
// на одну установку agctl (StorageID пользователей уникальны только внутри
// установки). Image должен лежать в реестре, доступном кластеру.
type Config struct {
	Namespace        string
	Kubeconfig       string
	ClusterDomain    string
	Image            string
	ImagePullPolicy  corev1.PullPolicy
	StorageClassName string
	StorageSize      string
	RuntimeClassName string
	CPURequest       string
	CPULimit         string
	MemoryRequest    string
	MemoryLimit      string

	// SharedClaim — имя общего RWX PersistentVolumeClaim (NFS/CephFS/EFS/
	// Filestore): данные пользователя лежат в подкаталоге <StorageID>/
	// (work, dsh-home), шлюз монтирует тот же том как $AGCTL_HOME/chats.
	// Пусто = PVC на пользователя (проектные API шлюза файлов не видят).
	SharedClaim string
	// Version — версия сборки agctl; поды помечаются меткой agctl.io/version,
	// SweepVersionDrift удаляет поды других версий (как Docker-свип дрейфа).
	Version string
	// WaitReady — ждать readiness пода в EnsureChat (ReadyTimeout, по
	// умолчанию 180 с). Без ожидания шлюз сам переживает прогрев (grace).
	WaitReady    bool
	ReadyTimeout time.Duration
	// SSHHost — внешний адрес узлов/балансировщика для SSH developer'а.
	// Непустое значение включает NodePort-сервис порта 2222 (под при этом
	// работает от root с минимальным набором capabilities для sshd);
	// пусто = SSH-канал в кластере выключен.
	SSHHost string
	// GatewaySelector — метки подов шлюза agctl: только им NetworkPolicy
	// разрешает ingress на dsh web. Пусто = любой под namespace.
	GatewaySelector map[string]string
	// Liveness — добавить livenessProbe (agctl-dsh-net health, как HEALTHCHECK
	// образа): зависший dsh перезапускается kubelet'ом.
	Liveness bool
}

// Execer выполняет команду в контейнере пода (pods/exec). Подменяется в тестах;
// в проде — SPDY-исполнитель client-go (см. exec.go).
type Execer interface {
	Exec(ctx context.Context, namespace, pod, container string, cmd []string) (string, error)
}

// Provider — Kubernetes-реализация runtime.ChatProvider (и legacy
// server.RuntimeIface для админских списков/логов — см. legacy.go).
type Provider struct {
	cfg    Config
	client kubernetes.Interface
	exec   Execer
	warned sync.Map // runtime name → предупреждение уже выдано
}

// New строит клиент из in-cluster credentials или kubeconfig.
func New(cfg Config) (*Provider, error) {
	var restConfig *rest.Config
	var err error
	if cfg.Kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("k8srt: load Kubernetes credentials: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("k8srt: create Kubernetes client: %w", err)
	}
	if cfg.Namespace == "" {
		cfg.Namespace = inClusterNamespace()
	}
	p, err := NewWithClient(cfg, client)
	if err != nil {
		return nil, err
	}
	p.exec = &spdyExecer{restConfig: restConfig, client: client}
	return p, nil
}

// inClusterNamespace — namespace пода шлюза из projected service-account
// (пусто вне кластера).
func inClusterNamespace() string {
	raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// NewWithClient — инъекция клиента client-go (тесты, встроенные контроллеры).
// Валидирует quantity-значения до любого обращения к кластеру.
func NewWithClient(cfg Config, client kubernetes.Interface) (*Provider, error) {
	if client == nil {
		return nil, fmt.Errorf("k8srt: Kubernetes client is required")
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if problems := validation.IsDNS1123Label(cfg.Namespace); len(problems) != 0 {
		return nil, fmt.Errorf("k8srt: invalid namespace %q: %s", cfg.Namespace, strings.Join(problems, ", "))
	}
	if cfg.Image == "" {
		return nil, fmt.Errorf("k8srt: DSH image is required")
	}
	if cfg.ClusterDomain == "" {
		cfg.ClusterDomain = "cluster.local"
	}
	if cfg.StorageSize == "" {
		cfg.StorageSize = "5Gi"
	}
	if cfg.CPURequest == "" {
		cfg.CPURequest = "100m"
	}
	if cfg.CPULimit == "" {
		cfg.CPULimit = "2"
	}
	if cfg.MemoryRequest == "" {
		cfg.MemoryRequest = "256Mi"
	}
	if cfg.MemoryLimit == "" {
		cfg.MemoryLimit = "2Gi"
	}
	if cfg.ImagePullPolicy == "" {
		cfg.ImagePullPolicy = corev1.PullIfNotPresent
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = defaultReadyTimeout
	}
	if cfg.SharedClaim != "" {
		if problems := validation.IsDNS1123Subdomain(cfg.SharedClaim); len(problems) != 0 {
			return nil, fmt.Errorf("k8srt: invalid shared claim name %q: %s", cfg.SharedClaim, strings.Join(problems, ", "))
		}
	}
	for key, value := range map[string]string{
		"storage size":   cfg.StorageSize,
		"cpu request":    cfg.CPURequest,
		"cpu limit":      cfg.CPULimit,
		"memory request": cfg.MemoryRequest,
		"memory limit":   cfg.MemoryLimit,
	} {
		if _, err := resource.ParseQuantity(value); err != nil {
			return nil, fmt.Errorf("k8srt: invalid %s %q: %w", key, value, err)
		}
	}
	return &Provider{cfg: cfg, client: client}, nil
}

// SetExecer подменяет исполнитель pods/exec (тесты HasActiveSSH).
func (p *Provider) SetExecer(e Execer) { p.exec = e }

// Namespace — namespace установки (для логов и манифестов).
func (p *Provider) Namespace() string { return p.cfg.Namespace }

// EnsureChat идемпотентно создаёт данные (PVC или подкаталог общего тома),
// Secret провайдеров, Service, NetworkPolicy и под dsh пользователя. Под
// другой версии/образа заменяется (данные не трогаются). Сервис остаётся
// приватным для кластера — шлюз ходит по его DNS-имени.
func (p *Provider) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	if spec.RuntimeID == "" || spec.StorageID == "" {
		return agctlrt.ChatHandle{}, fmt.Errorf("k8srt: runtime and storage IDs are required")
	}
	if p.cfg.SharedClaim != "" && !safeSubPath(spec.StorageID) {
		return agctlrt.ChatHandle{}, fmt.Errorf("k8srt: invalid storage id %q for shared claim", spec.StorageID)
	}
	name := resourceName(spec.RuntimeID)
	storageName := storageResourceName(spec.StorageID)
	runtimeLabels := map[string]string{
		labelName:       appName,
		labelManagedBy:  "agctl",
		labelRuntimeKey: name,
		labelStorageKey: labelValue(spec.StorageID),
		labelRuntimeID:  labelValue(spec.RuntimeID),
		labelUserID:     strconv.FormatInt(spec.UserID, 10),
		labelVersion:    labelValue(p.versionLabel()),
	}
	storageLabels := map[string]string{
		labelName:       appName,
		labelManagedBy:  "agctl",
		labelStorageKey: labelValue(spec.StorageID),
	}
	pvcName, serviceName, secretName := "data-"+storageName, "chat-"+name, "llm-"+storageName
	if p.cfg.SharedClaim != "" {
		pvcName = p.cfg.SharedClaim
	} else if err := p.ensurePVC(ctx, pvcName, storageLabels); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	secretName, err := p.ensureLLMSecret(ctx, secretName, storageLabels, spec.Provider.CredentialEnv())
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if err := p.ensureService(ctx, serviceName, runtimeLabels); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	sshEnabled := spec.SSHPubKey != "" && p.cfg.SSHHost != ""
	nodePort := 0
	if sshEnabled {
		nodePort, err = p.ensureSSHService(ctx, "ssh-"+name, runtimeLabels)
		if err != nil {
			return agctlrt.ChatHandle{}, err
		}
	} else if err := p.deleteIgnoreNotFound(func() error {
		return p.client.CoreV1().Services(p.cfg.Namespace).Delete(ctx, "ssh-"+name, metav1.DeleteOptions{})
	}); err != nil {
		return agctlrt.ChatHandle{}, fmt.Errorf("k8srt: delete SSH Service: %w", err)
	}
	if err := p.ensureNetworkPolicy(ctx, name, runtimeLabels, sshEnabled); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	pod, err := p.ensurePod(ctx, name, pvcName, secretName, runtimeLabels, spec, sshEnabled)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if p.cfg.WaitReady {
		if pod, err = p.waitReady(ctx, name); err != nil {
			return agctlrt.ChatHandle{}, err
		}
	}
	handle := agctlrt.ChatHandle{
		RuntimeID:  spec.RuntimeID,
		InstanceID: podInstanceID(pod),
		Endpoint:   fmt.Sprintf("http://%s.%s.svc.%s:80", serviceName, p.cfg.Namespace, p.cfg.ClusterDomain),
	}
	if sshEnabled && nodePort > 0 {
		handle.SSHHost, handle.SSHPort = p.cfg.SSHHost, nodePort
	}
	return handle, nil
}

// podInstanceID — идентификатор поколения: UID пода + счётчик рестартов
// контейнера (рестарт = новый boot-токен dsh).
func podInstanceID(pod *corev1.Pod) string {
	id := string(pod.UID)
	if id == "" {
		id = pod.Name
	}
	if len(pod.Status.ContainerStatuses) > 0 {
		id += "-restart-" + strconv.FormatInt(int64(pod.Status.ContainerStatuses[0].RestartCount), 10)
	}
	return id
}

// versionLabel — значение метки версии (dev, если версия не задана).
func (p *Provider) versionLabel() string {
	if p.cfg.Version == "" {
		return "dev"
	}
	return p.cfg.Version
}

// safeSubPath — StorageID пригоден как имя подкаталога общего тома.
func safeSubPath(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00") && !strings.HasPrefix(id, ".")
}

// waitPodReady (карточка k8s-chat-runtime-adapter) заменён upstream-парой
// waitReady + WaitReady/ReadyTimeout в Config: EnsureChat ждёт Pod Ready,
// сохраняя Pod/PVC для диагностики при таймауте (см. waitReady ниже).

// podReady — см. единая реализация ниже (upstream).

// ensurePVC создаёт PersistentVolumeClaim тома пользователя, если его нет
// (ReadWriteOnce; размер и storage-класс — из конфигурации провайдера).
func (p *Provider) ensurePVC(ctx context.Context, name string, labels map[string]string) error {
	api := p.client.CoreV1().PersistentVolumeClaims(p.cfg.Namespace)
	if _, err := api.Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: get PVC %s: %w", name, err)
	}
	size := resource.MustParse(p.cfg.StorageSize)
	spec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
	}
	if p.cfg.StorageClassName != "" {
		className := p.cfg.StorageClassName
		spec.StorageClassName = &className
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
		Spec:       spec,
	}
	if _, err := api.Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create PVC %s: %w", name, err)
	}
	return nil
}

// ensureLLMSecret кладёт ключи всех провайдеров в k8s Secret и возвращает имя
// секрета; пустой набор — секрет не создаётся (пустая строка).
func (p *Provider) ensureLLMSecret(ctx context.Context, name string, labels map[string]string, credentials map[string]string) (string, error) {
	if len(credentials) == 0 {
		return "", nil
	}
	data := make(map[string][]byte, len(credentials))
	for env, key := range credentials {
		if key != "" {
			data[env] = []byte(key)
		}
	}
	if len(data) == 0 {
		return "", nil
	}
	api := p.client.CoreV1().Secrets(p.cfg.Namespace)
	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		}
		if _, createErr := api.Create(ctx, secret, metav1.CreateOptions{}); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return "", fmt.Errorf("k8srt: create Secret %s: %w", name, createErr)
		}
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("k8srt: get Secret %s: %w", name, err)
	}
	same := len(existing.Data) == len(data)
	if same {
		for env, value := range data {
			if string(existing.Data[env]) != string(value) {
				same = false
				break
			}
		}
	}
	if same {
		return name, nil
	}
	existing.Data = data
	if _, err := api.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("k8srt: update Secret %s: %w", name, err)
	}
	return name, nil
}

// ensureService создаёт ClusterIP-сервис чат-пода, если сервиса нет.
func (p *Provider) ensureService(ctx context.Context, name string, labels map[string]string) error {
	api := p.client.CoreV1().Services(p.cfg.Namespace)
	if _, err := api.Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: get Service %s: %w", name, err)
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels(labels),
			Ports:    []corev1.ServicePort{{Name: "web", Port: 80, TargetPort: intstr.FromInt(webPort)}},
		},
	}
	if _, err := api.Create(ctx, service, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create Service %s: %w", name, err)
	}
	return nil
}

// ensureSSHService — NodePort-сервис порта sshd developer'а (только он:
// web-порт наружу не публикуется). Возвращает выделенный nodePort
// (0, если API ещё не выделил).
func (p *Provider) ensureSSHService(ctx context.Context, name string, labels map[string]string) (int, error) {
	api := p.client.CoreV1().Services(p.cfg.Namespace)
	svc, err := api.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		svc, err = api.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: selectorLabels(labels),
				Ports:    []corev1.ServicePort{{Name: "ssh", Port: 22, TargetPort: intstr.FromInt(sshPort)}},
			},
		}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			svc, err = api.Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return 0, fmt.Errorf("k8srt: SSH Service %s: %w", name, err)
	}
	for _, port := range svc.Spec.Ports {
		if port.Name == "ssh" {
			return int(port.NodePort), nil
		}
	}
	return 0, nil
}

// selectorLabels — метки, по которым сервис/политика находят под (без
// версии: обновление agctl не должно «отвязать» сервис от живого пода).
func selectorLabels(labels map[string]string) map[string]string {
	return map[string]string{labelName: labels[labelName], labelManagedBy: "agctl", labelRuntimeKey: labels[labelRuntimeKey]}
}

// ensureNetworkPolicy — ingress-гард пода: dsh web доступен только подам
// шлюза (GatewaySelector; пусто = любой под namespace), sshd — отовсюду, но
// только когда SSH включён (NodePort приходит с узла, а не из pod-сети).
// Egress не ограничивается: base URL LLM задаётся пользователем/профилем;
// политика egress — зона оператора кластера (см. docs/kubernetes.md).
func (p *Provider) ensureNetworkPolicy(ctx context.Context, name string, labels map[string]string, sshEnabled bool) error {
	api := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace)
	policyName := "net-" + name
	protocol := corev1.ProtocolTCP
	web := intstr.FromInt(webPort)
	from := []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: p.cfg.GatewaySelector}}}
	rules := []networkingv1.NetworkPolicyIngressRule{{
		From:  from,
		Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &web}},
	}}
	if sshEnabled {
		ssh := intstr.FromInt(sshPort)
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &ssh}},
		})
	}
	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: p.cfg.Namespace, Labels: labels},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabels(labels)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     rules,
		},
	}
	existing, err := api.Get(ctx, policyName, metav1.GetOptions{})
	if err == nil {
		if len(existing.Spec.Ingress) == len(rules) {
			return nil
		}
		existing.Spec = desired.Spec
		if _, err := api.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("k8srt: update NetworkPolicy %s: %w", policyName, err)
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: get NetworkPolicy %s: %w", policyName, err)
	}
	if _, err := api.Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create NetworkPolicy %s: %w", policyName, err)
	}
	return nil
}

// ensurePod возвращает живой чат-под. Terminal-поды и поды другого
// образа/версии удаляются и создаются заново (данные на томе не трогаются).
func (p *Provider) ensurePod(ctx context.Context, name, pvcName, secretName string, labels map[string]string, spec agctlrt.ChatSpec, sshEnabled bool) (*corev1.Pod, error) {
	api := p.client.CoreV1().Pods(p.cfg.Namespace)
	image := p.cfg.Image
	if spec.Image != "" {
		image = spec.Image
	}
	if existing, err := api.Get(ctx, name, metav1.GetOptions{}); err == nil {
		terminal := existing.Status.Phase == corev1.PodFailed || existing.Status.Phase == corev1.PodSucceeded
		drift := existing.Annotations[annotationImage] != image || existing.Labels[labelVersion] != labels[labelVersion]
		if !terminal && !drift {
			return existing, nil
		}
		if err := p.replacePod(ctx, name); err != nil {
			return nil, err
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("k8srt: get Pod %s: %w", name, err)
	}
	pod := p.buildPod(name, pvcName, secretName, image, labels, spec, sshEnabled)
	created, err := api.Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return api.Get(ctx, name, metav1.GetOptions{})
		}
		return nil, fmt.Errorf("k8srt: create Pod %s: %w", name, err)
	}
	return created, nil
}

// replacePod удаляет под и дожидается его исчезновения (ограничено ctx и
// ReadyTimeout), чтобы создать замену под тем же именем.
func (p *Provider) replacePod(ctx context.Context, name string) error {
	api := p.client.CoreV1().Pods(p.cfg.Namespace)
	grace := int64(30)
	if err := api.Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: delete Pod %s: %w", name, err)
	}
	deadline := time.Now().Add(p.cfg.ReadyTimeout)
	for {
		if _, err := api.Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("k8srt: Pod %s is still terminating; retry request", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// buildPod — спецификация пода dsh пользователя.
func (p *Provider) buildPod(name, pvcName, secretName, image string, labels map[string]string, spec agctlrt.ChatSpec, sshEnabled bool) *corev1.Pod {
	modelsJSON, _ := json.Marshal(spec.Provider.AllModels())
	providerEnv := []corev1.EnvVar{
		{Name: "AGCTL_LLM_PROVIDER", Value: spec.Provider.Alias()},
		{Name: "AGCTL_LLM_BASE_URL", Value: spec.Provider.BaseURL},
		{Name: "AGCTL_LLM_MODEL", Value: spec.Provider.Model},
		{Name: "AGCTL_LLM_MODELS", Value: string(modelsJSON)},
	}
	if spec.Provider.APIKey != "" && secretName != "" {
		providerEnv = append(providerEnv, secretEnv("AGCTL_LLM_API_KEY", secretName))
	}
	// Дополнительные провайдеры: каждый ключ — из своего ключа Secret; в
	// init-контейнер передаём только описание (без секретов).
	type extraSpec struct {
		Name      string
		BaseURL   string
		Model     string
		Models    []string
		APIKeyEnv string
	}
	extras := make([]extraSpec, 0, len(spec.Provider.Extras))
	for _, extra := range spec.Provider.Extras {
		if len(extra.AllModels()) == 0 || strings.TrimSpace(extra.BaseURL) == "" {
			continue
		}
		extras = append(extras, extraSpec{Name: extra.Name, BaseURL: extra.BaseURL, Model: extra.DefaultModel(), Models: extra.AllModels(), APIKeyEnv: extra.KeyEnv()})
		if extra.APIKey != "" && secretName != "" {
			providerEnv = append(providerEnv, secretEnv(extra.KeyEnv(), secretName))
		}
	}
	if len(extras) > 0 {
		if raw, err := json.Marshal(extras); err == nil {
			providerEnv = append(providerEnv, corev1.EnvVar{Name: "AGCTL_LLM_EXTRA_PROVIDERS", Value: string(raw)})
		}
	}
	refresh := "0"
	if spec.Provider.APIKey != "" || spec.Provider.BaseURL != "" || spec.Provider.Model != "" || len(extras) > 0 {
		refresh = "1"
	}
	initEnv := append(append([]corev1.EnvVar(nil), providerEnv...), corev1.EnvVar{Name: "AGCTL_REFRESH_PROVIDER", Value: refresh})
	if spec.DefaultFullAccess {
		initEnv = append(initEnv, corev1.EnvVar{Name: "AGCTL_DEFAULT_FULL_ACCESS", Value: "1"})
	}
	// Данные: PVC на пользователя (subPath work/dsh-home) или общий том
	// (subPath <StorageID>/work, <StorageID>/dsh-home; init видит <StorageID>).
	prefix := ""
	if p.cfg.SharedClaim != "" {
		prefix = spec.StorageID + "/"
	}
	volume := corev1.Volume{Name: "chat-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName}}}
	rootMount := corev1.VolumeMount{Name: "chat-data", MountPath: "/data", SubPath: strings.TrimSuffix(prefix, "/")}

	runAsNonRoot, allowEscalation := true, false
	uid, gid := int64(1000), int64(1000)
	caps := &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}
	if sshEnabled {
		// sshd: ssh-keygen -A пишет /etc/ssh, privsep требует chroot и смены
		// uid/gid — под работает от root с минимальным набором capabilities.
		// Рекомендуется RuntimeClass gVisor (см. docs/kubernetes.md).
		runAsNonRoot, uid, gid = false, 0, 0
		caps.Add = []corev1.Capability{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID", "SYS_CHROOT", "KILL", "AUDIT_WRITE"}
	}
	containerSecurity := func() *corev1.SecurityContext {
		return &corev1.SecurityContext{
			RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid, RunAsGroup: &gid,
			AllowPrivilegeEscalation: &allowEscalation,
			Capabilities:             caps,
		}
	}
	init := corev1.Container{
		Name: "seed-dsh-home", Image: image, ImagePullPolicy: p.cfg.ImagePullPolicy,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"mkdir -p /data/work /data/dsh-home && /usr/local/bin/agctl init-chat-home --home /data/dsh-home"},
		Env:     initEnv, VolumeMounts: []corev1.VolumeMount{rootMount},
		SecurityContext: containerSecurity(),
	}
	mainEnv := []corev1.EnvVar{
		{Name: "DSH_HOME", Value: "/home/dsh"},
		{Name: "AGCTL_WS_ID", Value: spec.RuntimeID},
		{Name: "HOME", Value: "/workspace"},
		{Name: "NPM_CONFIG_CACHE", Value: "/tmp/npm-cache"},
		{Name: "XDG_CACHE_HOME", Value: "/tmp/cache"},
	}
	mainEnv = append(mainEnv, providerEnv...)
	// Доменные переменные developer'а (KEY=VALUE из .env хоста шлюза).
	for _, kv := range spec.DevEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.TrimSpace(k) != "" {
			mainEnv = append(mainEnv, corev1.EnvVar{Name: strings.TrimSpace(k), Value: v})
		}
	}
	ports := []corev1.ContainerPort{{Name: "web", ContainerPort: webPort}}
	if sshEnabled {
		mainEnv = append(mainEnv, corev1.EnvVar{Name: "AGCTL_SSH_PUBKEY", Value: spec.SSHPubKey})
		ports = append(ports, corev1.ContainerPort{Name: "ssh", ContainerPort: sshPort})
	}
	container := corev1.Container{
		Name: "dsh", Image: image, ImagePullPolicy: p.cfg.ImagePullPolicy,
		Ports: ports,
		Env:   mainEnv,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "chat-data", MountPath: "/workspace", SubPath: prefix + "work"},
			{Name: "chat-data", MountPath: "/home/dsh", SubPath: prefix + "dsh-home"},
		},
		Resources: p.resources(),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("web")}},
			InitialDelaySeconds: 2, PeriodSeconds: 3, FailureThreshold: 20,
		},
		SecurityContext: containerSecurity(),
	}
	if p.cfg.Liveness {
		container.LivenessProbe = &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"node", "/usr/local/bin/agctl-dsh-net", "health"}}},
			InitialDelaySeconds: 150, PeriodSeconds: 30, TimeoutSeconds: 6, FailureThreshold: 3,
		}
	}
	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyAlways,
		AutomountServiceAccountToken: ptr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid, RunAsGroup: &gid,
			FSGroup: ptr(int64(1000)), FSGroupChangePolicy: ptr(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		InitContainers: []corev1.Container{init},
		Containers:     []corev1.Container{container},
		Volumes:        []corev1.Volume{volume},
	}
	if p.cfg.RuntimeClassName != "" {
		className := p.cfg.RuntimeClassName
		podSpec.RuntimeClassName = &className
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: p.cfg.Namespace, Labels: labels,
			Annotations: map[string]string{annotationImage: image},
		},
		Spec: podSpec,
	}
}

// secretEnv — переменная окружения из ключа Secret.
func secretEnv(name, secretName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: name,
		}},
	}
}

// resources — requests/limits CPU и памяти пода из конфигурации провайдера.
func (p *Provider) resources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(p.cfg.CPURequest),
			corev1.ResourceMemory: resource.MustParse(p.cfg.MemoryRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(p.cfg.CPULimit),
			corev1.ResourceMemory: resource.MustParse(p.cfg.MemoryLimit),
		},
	}
}

// waitReady ждёт условие Ready пода (ReadyTimeout). Упавший под удаляется с
// ошибкой «retry», в ошибке таймаута — причина ожидания контейнера.
func (p *Provider) waitReady(ctx context.Context, name string) (*corev1.Pod, error) {
	api := p.client.CoreV1().Pods(p.cfg.Namespace)
	deadline := time.Now().Add(p.cfg.ReadyTimeout)
	var last *corev1.Pod
	for {
		pod, err := api.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("k8srt: get Pod %s: %w", name, err)
		}
		last = pod
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			_ = api.Delete(ctx, name, metav1.DeleteOptions{})
			return nil, fmt.Errorf("k8srt: Pod %s terminated (%s: %s); retry request", name, pod.Status.Phase, podWaitingReason(pod))
		}
		if podReady(pod) {
			return pod, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("k8srt: Pod %s not ready within %s (%s)", name, p.cfg.ReadyTimeout, podWaitingReason(last))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(readyPollInterval):
		}
	}
}

// podReady — условие Ready=True.
func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// podWaitingReason — причина/сообщение ожидания первого контейнера (для
// ошибок и статуса): ImagePullBackOff, CrashLoopBackOff и т. п.
func podWaitingReason(pod *corev1.Pod) string {
	if pod == nil {
		return "unknown"
	}
	statuses := append(append([]corev1.ContainerStatus(nil), pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return strings.TrimSpace(cs.State.Waiting.Reason + " " + cs.State.Waiting.Message)
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return strings.TrimSpace(cs.State.Terminated.Reason + " " + cs.State.Terminated.Message)
		}
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	return string(pod.Status.Phase)
}

// StopChat удаляет compute (под, сервисы, политику), но намеренно оставляет
// PVC пользователя и Secret провайдера для следующего ленивого старта.
func (p *Provider) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	return p.deleteRuntime(ctx, resourceName(handle.RuntimeID))
}

// deleteRuntime — удаление всех compute-ресурсов рантайма по имени пода.
func (p *Provider) deleteRuntime(ctx context.Context, name string) error {
	var errs []error
	if err := p.client.CoreV1().Pods(p.cfg.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete Pod: %w", err))
	}
	for _, svc := range []string{"chat-" + name, "ssh-" + name} {
		if err := p.client.CoreV1().Services(p.cfg.Namespace).Delete(ctx, svc, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete Service %s: %w", svc, err))
		}
	}
	if err := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace).Delete(ctx, "net-"+name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete NetworkPolicy: %w", err))
	}
	return errors.Join(errs...)
}

func (p *Provider) deleteIgnoreNotFound(del func() error) error {
	if err := del(); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// LogsChat — хвост логов контейнера dsh: источник boot-токена для mint
// cookie, тот же путь, что у Docker. Токен в лог agctl не попадает.
func (p *Provider) LogsChat(ctx context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	return p.podLogs(ctx, resourceName(handle.RuntimeID), tail)
}

func (p *Provider) podLogs(ctx context.Context, name, tail string) (string, error) {
	lines := int64(100)
	if n, err := strconv.ParseInt(tail, 10, 64); err == nil && n > 0 {
		lines = n
	}
	request := p.client.CoreV1().Pods(p.cfg.Namespace).GetLogs(name, &corev1.PodLogOptions{Container: "dsh", TailLines: &lines})
	data, err := request.Do(ctx).Raw()
	if err != nil {
		return "", fmt.Errorf("k8srt: read Pod logs %s: %w", name, err)
	}
	return string(data), nil
}

// HasActiveSSH — есть ли живой sshd-сеанс developer'а: `ps -eo pid,args`
// через pods/exec. Без SSH-канала (SSHPort = 0) всегда false; ошибка exec
// возвращается как есть — рипер при ошибке откладывает усыпление.
func (p *Provider) HasActiveSSH(ctx context.Context, handle agctlrt.ChatHandle) (bool, error) {
	if handle.SSHPort == 0 {
		return false, nil
	}
	if p.exec == nil {
		return false, fmt.Errorf("k8srt: pods/exec is not configured")
	}
	out, err := p.exec.Exec(ctx, p.cfg.Namespace, resourceName(handle.RuntimeID), "dsh", []string{"ps", "-eo", "pid,args"})
	if err != nil {
		return false, err
	}
	return agctlrt.ActiveSSHSession(out), nil
}

// HealthStatus — состояние пода по идентификатору поколения (podInstanceID)
// или имени пода: "unhealthy" для удалённых/упавших/зацикленных
// (CrashLoopBackOff, ImagePullBackOff) подов, иначе "healthy". Сервер
// сбрасывает кэш unhealthy-рантайма и лениво пересоздаёт его на том же томе.
func (p *Provider) HealthStatus(ctx context.Context, instanceID string) (string, error) {
	pod, err := p.findPod(ctx, instanceID)
	if err != nil {
		return "", err
	}
	if pod == nil {
		return "unhealthy", nil
	}
	return podHealth(pod), nil
}

// podHealth — классификация состояния пода для reconcile.
func podHealth(pod *corev1.Pod) string {
	if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		return "unhealthy"
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			switch cs.State.Waiting.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName":
				return "unhealthy"
			}
		}
	}
	return "healthy"
}

// findPod — под по имени или по UID-префиксу podInstanceID; nil = не найден.
func (p *Provider) findPod(ctx context.Context, id string) (*corev1.Pod, error) {
	api := p.client.CoreV1().Pods(p.cfg.Namespace)
	if strings.HasPrefix(id, "agctl-chat-") {
		pod, err := api.Get(ctx, id, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("k8srt: get Pod %s: %w", id, err)
		}
		return pod, nil
	}
	uid, _, _ := strings.Cut(id, "-restart-")
	list, err := api.List(ctx, metav1.ListOptions{LabelSelector: p.chatSelector()})
	if err != nil {
		return nil, fmt.Errorf("k8srt: list Pods: %w", err)
	}
	for i := range list.Items {
		if string(list.Items[i].UID) == uid || list.Items[i].Name == uid {
			return &list.Items[i], nil
		}
	}
	return nil, nil
}

// chatSelector — селектор всех чат-подов этой установки.
func (p *Provider) chatSelector() string {
	return labelName + "=" + appName + "," + labelManagedBy + "=agctl"
}

// SweepVersionDrift удаляет compute-ресурсы подов другой версии agctl
// (новый серв не знает их, рипер не усыпит, а они раздают старый dsh);
// данные пользователей (тома, Secret) не трогаются. Возвращает число
// удалённых рантаймов.
func (p *Provider) SweepVersionDrift(ctx context.Context) int {
	list, err := p.client.CoreV1().Pods(p.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: p.chatSelector()})
	if err != nil {
		return 0
	}
	current := labelValue(p.versionLabel())
	n := 0
	for _, pod := range list.Items {
		if pod.Labels[labelVersion] == current {
			continue
		}
		if err := p.deleteRuntime(ctx, pod.Name); err == nil {
			n++
		}
	}
	return n
}

// ListRuntimes — чат-поды установки, отсортированные по имени (админка).
func (p *Provider) ListRuntimes(ctx context.Context) ([]corev1.Pod, error) {
	list, err := p.client.CoreV1().Pods(p.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: p.chatSelector()})
	if err != nil {
		return nil, fmt.Errorf("k8srt: list Pods: %w", err)
	}
	pods := list.Items
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods, nil
}

// resourceName — имя чат-пода детерминировано из runtimeID (sha256, 8 hex):
// новая генерация рантайма — новое имя.
func resourceName(runtimeID string) string {
	sum := sha256.Sum256([]byte(runtimeID))
	return "agctl-chat-" + hex.EncodeToString(sum[:8])
}

// storageResourceName — имена PVC/Secret из стабильного StorageID, а не из
// сменяемого поколения рантайма. Namespace — одна установка agctl, иначе
// одинаковые StorageID пользователей столкнутся.
func storageResourceName(storageID string) string {
	sum := sha256.Sum256([]byte(storageID))
	return "agctl-storage-" + hex.EncodeToString(sum[:8])
}

// labelValue ужимает значение до валидной k8s-метки (≤63 символа;
// невалидное → короткий хеш).
func labelValue(value string) string {
	if len(value) <= 63 && validation.IsValidLabelValue(value) == nil {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func ptr[T any](value T) *T { return &value }
