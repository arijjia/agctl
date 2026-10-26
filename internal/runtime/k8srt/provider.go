// Package k8srt implements the ChatProvider contract with Kubernetes Pods,
// ClusterIP Services and per-user PersistentVolumeClaims. It deliberately
// deletes compute on idle without deleting the user's PVC.
package k8srt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

const webPort = 3080

// Config controls the namespaced K8s runtime. Namespace is the storage scope:
// use a dedicated namespace per agctl installation unless StorageIDs are made
// globally unique. The image must be pushed to a registry reachable by the
// cluster; local Docker tags are not sufficient.
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
}

// Provider is a Kubernetes-backed implementation of runtime.ChatProvider.
type Provider struct {
	cfg    Config
	client kubernetes.Interface
}

// New builds a client from in-cluster credentials or the requested kubeconfig.
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
	return NewWithClient(cfg, client)
}

// NewWithClient allows tests and embedding controllers to inject a client-go
// client. It validates resource quantities before any cluster mutation.
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

// EnsureChat idempotently creates a user's PVC, Service, and DSH Pod. The
// Service remains private to the cluster; the agctl gateway uses its DNS name.
func (p *Provider) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	if spec.RuntimeID == "" || spec.StorageID == "" {
		return agctlrt.ChatHandle{}, fmt.Errorf("k8srt: runtime and storage IDs are required")
	}
	name := resourceName(spec.RuntimeID)
	storageName := storageResourceName(spec.StorageID)
	runtimeLabels := map[string]string{
		"app.kubernetes.io/name":       "agctl-dsh-chat",
		"app.kubernetes.io/managed-by": "agctl",
		"agctl.io/runtime-key":         name,
		"agctl.io/storage-key":         labelValue(spec.StorageID),
	}
	storageLabels := map[string]string{
		"app.kubernetes.io/name":       "agctl-dsh-chat",
		"app.kubernetes.io/managed-by": "agctl",
		"agctl.io/storage-key":         labelValue(spec.StorageID),
	}
	pvcName, serviceName, secretName := "data-"+storageName, "chat-"+name, "llm-"+storageName

	if err := p.ensurePVC(ctx, pvcName, storageLabels); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	secretName, err := p.ensureLLMSecret(ctx, secretName, storageLabels, spec.Provider.APIKey)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if err := p.ensureService(ctx, serviceName, runtimeLabels); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if err := p.ensureNetworkPolicy(ctx, name, runtimeLabels); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	pod, err := p.ensurePod(ctx, name, pvcName, secretName, runtimeLabels, spec)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	instanceID := string(pod.UID)
	if instanceID == "" {
		instanceID = pod.Name
	}
	if len(pod.Status.ContainerStatuses) > 0 {
		instanceID += "-restart-" + strconv.FormatInt(int64(pod.Status.ContainerStatuses[0].RestartCount), 10)
	}
	endpoint := fmt.Sprintf("http://%s.%s.svc.%s:80", serviceName, p.cfg.Namespace, p.cfg.ClusterDomain)
	return agctlrt.ChatHandle{RuntimeID: spec.RuntimeID, InstanceID: instanceID, Endpoint: endpoint}, nil
}

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

func (p *Provider) ensureLLMSecret(ctx context.Context, name string, labels map[string]string, apiKey string) (string, error) {
	if apiKey == "" {
		return "", nil
	}
	api := p.client.CoreV1().Secrets(p.cfg.Namespace)
	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"AGCTL_LLM_API_KEY": []byte(apiKey)},
		}
		if _, createErr := api.Create(ctx, secret, metav1.CreateOptions{}); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return "", fmt.Errorf("k8srt: create Secret %s: %w", name, createErr)
		}
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("k8srt: get Secret %s: %w", name, err)
	}
	if string(existing.Data["AGCTL_LLM_API_KEY"]) == apiKey {
		return name, nil
	}
	existing.Data = map[string][]byte{"AGCTL_LLM_API_KEY": []byte(apiKey)}
	if _, err := api.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("k8srt: update Secret %s: %w", name, err)
	}
	return name, nil
}

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
			Selector: labels,
			Ports:    []corev1.ServicePort{{Name: "web", Port: 80, TargetPort: intstr.FromInt(webPort)}},
		},
	}
	if _, err := api.Create(ctx, service, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create Service %s: %w", name, err)
	}
	return nil
}

// ensureNetworkPolicy is a prototype ingress guard: it permits any Pod in the
// configured namespace to reach the relay port. It is not gateway-specific,
// does not constrain egress, and requires target-CNI validation/hardening before
// production use because LLM base URLs are user-configured.
func (p *Provider) ensureNetworkPolicy(ctx context.Context, name string, labels map[string]string) error {
	api := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace)
	policyName := "net-" + name
	if _, err := api.Get(ctx, policyName, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8srt: get NetworkPolicy %s: %w", policyName, err)
	}
	protocol := corev1.ProtocolTCP
	port := intstr.FromInt(webPort)
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: p.cfg.Namespace, Labels: labels},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: labels},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
			}},
		},
	}
	if _, err := api.Create(ctx, policy, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("k8srt: create NetworkPolicy %s: %w", policyName, err)
	}
	return nil
}

func (p *Provider) ensurePod(ctx context.Context, name, pvcName, secretName string, labels map[string]string, spec agctlrt.ChatSpec) (*corev1.Pod, error) {
	api := p.client.CoreV1().Pods(p.cfg.Namespace)
	if existing, err := api.Get(ctx, name, metav1.GetOptions{}); err == nil {
		if existing.Status.Phase == corev1.PodFailed || existing.Status.Phase == corev1.PodSucceeded {
			if err := api.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("k8srt: delete terminal Pod %s: %w", name, err)
			}
			return nil, fmt.Errorf("k8srt: Pod %s was terminal and is being replaced; retry request", name)
		}
		return existing, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("k8srt: get Pod %s: %w", name, err)
	}

	modelsJSON, _ := json.Marshal(spec.Provider.AllModels())
	providerEnv := []corev1.EnvVar{
		{Name: "AGCTL_LLM_BASE_URL", Value: spec.Provider.BaseURL},
		{Name: "AGCTL_LLM_MODEL", Value: spec.Provider.Model},
		{Name: "AGCTL_LLM_MODELS", Value: string(modelsJSON)},
	}
	if spec.Provider.APIKey != "" && secretName != "" {
		providerEnv = append(providerEnv, corev1.EnvVar{
			Name: "AGCTL_LLM_API_KEY",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "AGCTL_LLM_API_KEY",
			}},
		})
	}
	refresh := "0"
	if spec.Provider.APIKey != "" || spec.Provider.BaseURL != "" || spec.Provider.Model != "" {
		refresh = "1"
	}
	initEnv := append(append([]corev1.EnvVar(nil), providerEnv...), corev1.EnvVar{Name: "AGCTL_REFRESH_PROVIDER", Value: refresh})
	volume := corev1.Volume{Name: "chat-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName}}}
	rootMount := corev1.VolumeMount{Name: "chat-data", MountPath: "/data"}
	runAsNonRoot, allowEscalation := true, false
	uid, gid := int64(1000), int64(1000)
	init := corev1.Container{
		Name: "seed-dsh-home", Image: p.cfg.Image, ImagePullPolicy: p.cfg.ImagePullPolicy,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"mkdir -p /data/work /data/dsh-home && /usr/local/bin/agctl init-chat-home --home /data/dsh-home"},
		Env:     initEnv, VolumeMounts: []corev1.VolumeMount{rootMount},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid, RunAsGroup: &gid,
			AllowPrivilegeEscalation: &allowEscalation,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
	mainEnv := []corev1.EnvVar{
		{Name: "DSH_HOME", Value: "/home/dsh"},
		{Name: "AGCTL_WS_ID", Value: spec.RuntimeID},
		{Name: "HOME", Value: "/workspace"},
		{Name: "NPM_CONFIG_CACHE", Value: "/tmp/npm-cache"},
		{Name: "XDG_CACHE_HOME", Value: "/tmp/cache"},
	}
	mainEnv = append(mainEnv, providerEnv...)
	container := corev1.Container{
		Name: "dsh", Image: p.cfg.Image, ImagePullPolicy: p.cfg.ImagePullPolicy,
		Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: webPort}},
		Env:   mainEnv,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "chat-data", MountPath: "/workspace", SubPath: "work"},
			{Name: "chat-data", MountPath: "/home/dsh", SubPath: "dsh-home"},
		},
		Resources: p.resources(),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("web")}},
			InitialDelaySeconds: 2, PeriodSeconds: 3, FailureThreshold: 20,
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid, RunAsGroup: &gid,
			AllowPrivilegeEscalation: &allowEscalation,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyAlways,
		AutomountServiceAccountToken: ptr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid, RunAsGroup: &gid,
			FSGroup: &gid, FSGroupChangePolicy: ptr(corev1.FSGroupChangeOnRootMismatch),
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
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.cfg.Namespace, Labels: labels},
		Spec:       podSpec,
	}
	created, err := api.Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return api.Get(ctx, name, metav1.GetOptions{})
		}
		return nil, fmt.Errorf("k8srt: create Pod %s: %w", name, err)
	}
	return created, nil
}

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

// StopChat deletes ephemeral compute and the Service, but deliberately leaves
// the user PVC and provider Secret in place for the next lazy start.
func (p *Provider) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	name := resourceName(handle.RuntimeID)
	var errs []error
	if err := p.client.CoreV1().Pods(p.cfg.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete Pod: %w", err))
	}
	if err := p.client.CoreV1().Services(p.cfg.Namespace).Delete(ctx, "chat-"+name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete Service: %w", err))
	}
	if err := p.client.NetworkingV1().NetworkPolicies(p.cfg.Namespace).Delete(ctx, "net-"+name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Errorf("delete NetworkPolicy: %w", err))
	}
	return errors.Join(errs...)
}

// LogsChat provides the DSH startup URL used by the same boot-cookie minting
// path as Docker; token lines are not logged by this provider.
func (p *Provider) LogsChat(ctx context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	lines := int64(100)
	if n, err := strconv.ParseInt(tail, 10, 64); err == nil && n > 0 {
		lines = n
	}
	name := resourceName(handle.RuntimeID)
	request := p.client.CoreV1().Pods(p.cfg.Namespace).GetLogs(name, &corev1.PodLogOptions{Container: "dsh", TailLines: &lines})
	data, err := request.Do(ctx).Raw()
	if err != nil {
		return "", fmt.Errorf("k8srt: read Pod logs %s: %w", name, err)
	}
	return string(data), nil
}

// HasActiveSSH is a no-op for the current K8s prototype: EnsureChat does not
// publish an SSH port (ChatHandle.SSHPort remains zero). Implement process
// inspection before enabling SSH for this provider.
func (p *Provider) HasActiveSSH(ctx context.Context, handle agctlrt.ChatHandle) (bool, error) {
	return false, nil
}

func resourceName(runtimeID string) string {
	sum := sha256.Sum256([]byte(runtimeID))
	return "agctl-chat-" + hex.EncodeToString(sum[:8])
}

// storageResourceName derives PVC/Secret names from the stable StorageID, not
// the replaceable runtime generation. Namespace must be scoped to one agctl
// installation so identical per-user StorageIDs cannot collide.
func storageResourceName(storageID string) string {
	sum := sha256.Sum256([]byte(storageID))
	return "agctl-storage-" + hex.EncodeToString(sum[:8])
}

func labelValue(value string) string {
	if len(value) <= 63 && validation.IsValidLabelValue(value) == nil {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func ptr[T any](value T) *T { return &value }
