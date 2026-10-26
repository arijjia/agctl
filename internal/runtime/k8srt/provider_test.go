package k8srt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureChatCreatesPodServiceAndPersistentPVC(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{
		Namespace: "agctl", Image: "registry.example.invalid/agctl-dsh:0.2.0",
		StorageClassName: "fast-rwo", StorageSize: "3Gi", RuntimeClassName: "gvisor",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	apiKey := hex.EncodeToString(keyBytes) // ephemeral test value; never logged or committed
	spec := agctlrt.ChatSpec{
		RuntimeID: "chat-a1b2c3-u17", StorageID: "u17", UserID: 17,
		Provider: provision.Provider{
			BaseURL: "https://llm.example.invalid/v1", Model: "model-a", APIKey: apiKey,
		},
	}
	handle, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	name := resourceName(spec.RuntimeID)
	storageName := storageResourceName(spec.StorageID)
	wantEndpoint := "http://chat-" + name + ".agctl.svc.cluster.local:80"
	if handle.RuntimeID != spec.RuntimeID || handle.Endpoint != wantEndpoint {
		t.Fatalf("unexpected handle: %+v", handle)
	}

	pvc, err := client.CoreV1().PersistentVolumeClaims("agctl").Get(ctx, "data-"+storageName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast-rwo" {
		t.Fatalf("storage class not set: %+v", pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; got.Cmp(resource.MustParse("3Gi")) != 0 {
		t.Fatalf("PVC size = %s, want 3Gi", got.String())
	}
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Fatalf("unexpected PVC access modes: %v", pvc.Spec.AccessModes)
	}

	svc, err := client.CoreV1().Services("agctl").Get(ctx, "chat-"+name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP || len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].NodePort != 0 {
		t.Fatalf("service must be cluster-internal, got: %+v", svc.Spec)
	}

	policy, err := client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "net-"+name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("NetworkPolicy must restrict ingress only: %+v", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Ingress) != 1 || len(policy.Spec.Ingress[0].Ports) != 1 || policy.Spec.Ingress[0].Ports[0].Port.IntValue() != 3080 {
		t.Fatalf("NetworkPolicy must allow only the relay port: %+v", policy.Spec.Ingress)
	}
	for _, peer := range policy.Spec.Ingress[0].From {
		if peer.PodSelector == nil {
			t.Fatalf("ingress must come from same-namespace pods only: %+v", policy.Spec.Ingress[0].From)
		}
	}

	pod, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "gvisor" {
		t.Fatalf("RuntimeClassName = %v, want gvisor", pod.Spec.RuntimeClassName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("runtime pod must not automount a service-account token")
	}
	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
		t.Fatal("runtime pod must run as non-root")
	}
	if pod.Spec.SecurityContext.SeccompProfile == nil || pod.Spec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatal("runtime pod must use the runtime-default seccomp profile")
	}
	if len(pod.Spec.Containers) != 1 || len(pod.Spec.Containers[0].Ports) != 1 || pod.Spec.Containers[0].Ports[0].HostPort != 0 {
		t.Fatalf("DSH container must not publish a hostPort: %+v", pod.Spec.Containers)
	}
	mounts := pod.Spec.Containers[0].VolumeMounts
	if len(mounts) != 2 || mounts[0].SubPath != "work" || mounts[1].SubPath != "dsh-home" {
		t.Fatalf("expected work and dsh-home subpaths in one persistent claim: %+v", mounts)
	}
	if len(pod.Spec.InitContainers) != 1 || pod.Spec.InitContainers[0].Name != "seed-dsh-home" {
		t.Fatalf("expected DSH_HOME init container, got %+v", pod.Spec.InitContainers)
	}
	secretName := "llm-" + storageName
	secret, err := client.CoreV1().Secrets("agctl").Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["AGCTL_LLM_API_KEY"]) != apiKey {
		t.Fatal("Kubernetes Secret did not receive the provider value")
	}
	for _, container := range []corev1.Container{pod.Spec.InitContainers[0], pod.Spec.Containers[0]} {
		found := false
		for _, env := range container.Env {
			if env.Name == "AGCTL_LLM_API_KEY" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == secretName {
				found = true
			}
		}
		if !found {
			t.Fatalf("container %s must read the API key from a Secret reference", container.Name)
		}
	}

	// EnsureChat is idempotent by RuntimeID, and idle stop removes compute only.
	again, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if again.Endpoint != handle.Endpoint {
		t.Fatalf("idempotent ensure changed endpoint: %q != %q", again.Endpoint, handle.Endpoint)
	}
	podCreates := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "pods" {
			podCreates++
		}
	}
	if podCreates != 1 {
		t.Fatalf("EnsureChat created %d Pods, want 1", podCreates)
	}
	if err := provider.StopChat(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Pod remains after StopChat: %v", err)
	}
	if _, err := client.CoreV1().Services("agctl").Get(ctx, "chat-"+name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Service remains after StopChat: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "net-"+name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("NetworkPolicy remains after StopChat: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("agctl").Get(ctx, "data-"+storageName, metav1.GetOptions{}); err != nil {
		t.Fatalf("PVC should survive idle stop: %v", err)
	}
	if _, err := client.CoreV1().Secrets("agctl").Get(ctx, secretName, metav1.GetOptions{}); err != nil {
		t.Fatalf("provider Secret should be retained for next start: %v", err)
	}
}

func TestPersistentResourcesFollowStorageIDAcrossRuntimeReplacement(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "registry.example.invalid/agctl-dsh:0.2.0"}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := agctlrt.ChatSpec{
		RuntimeID: "runtime-generation-one", StorageID: "stable-user-storage", UserID: 17,
		Provider: provision.Provider{BaseURL: "https://llm.example.invalid/v1", Model: "model-a", APIKey: "unit-test-key-1"},
	}
	first, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	storageName := storageResourceName(spec.StorageID)
	pvcName, secretName := "data-"+storageName, "llm-"+storageName
	firstPVC, err := client.CoreV1().PersistentVolumeClaims("agctl").Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, tiedToRuntime := firstPVC.Labels["agctl.io/runtime-key"]; tiedToRuntime {
		t.Fatal("persistent PVC must not be labeled as belonging to one runtime generation")
	}

	// Replacing compute changes RuntimeID/service, but must reuse the stable
	// StorageID's PVC and provider Secret.
	spec.RuntimeID = "runtime-generation-two"
	spec.Provider.APIKey = "unit-test-key-2"
	second, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.Endpoint == second.Endpoint || second.RuntimeID != spec.RuntimeID {
		t.Fatalf("runtime replacement did not create distinct compute endpoints: first=%+v second=%+v", first, second)
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims("agctl").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pvcs.Items) != 1 || pvcs.Items[0].Name != pvcName {
		t.Fatalf("PVCs after runtime replacement = %v, want one stable PVC %s", pvcs.Items, pvcName)
	}
	secret, err := client.CoreV1().Secrets("agctl").Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["AGCTL_LLM_API_KEY"]) != "unit-test-key-2" {
		t.Fatal("stable Secret was not refreshed for the replacement runtime")
	}

	secondName := resourceName(spec.RuntimeID)
	pod, err := client.CoreV1().Pods("agctl").Get(ctx, secondName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundPVC, foundSecret := false, false
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
			foundPVC = true
		}
	}
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "AGCTL_LLM_API_KEY" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == secretName {
			foundSecret = true
		}
	}
	if !foundPVC || !foundSecret {
		t.Fatalf("replacement Pod mounts stable storage/Secret: pvc=%v secret=%v", foundPVC, foundSecret)
	}
	if err := provider.StopChat(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, secondName, metav1.GetOptions{}); err != nil {
		t.Fatalf("stopping old generation removed replacement Pod: %v", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("agctl").Get(ctx, pvcName, metav1.GetOptions{}); err != nil {
		t.Fatalf("stable PVC removed with old generation: %v", err)
	}
}
