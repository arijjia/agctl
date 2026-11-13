package k8srt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func baseSpec() agctlrt.ChatSpec {
	return agctlrt.ChatSpec{
		RuntimeID: "chat-abcd1234-u17", StorageID: "u17", UserID: 17,
		Provider: provision.Provider{BaseURL: "https://llm.example.invalid/v1", Model: "model-a", APIKey: "unit-test-key"},
	}
}

func envValue(c corev1.Container, name string) (string, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func TestSharedClaimMountsUserSubdirectoriesWithoutPVC(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", SharedClaim: "agctl-data"}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	spec.DefaultFullAccess = true
	spec.DevEnv = []string{"GITLAB_URL=https://gitlab.example.invalid", "broken"}
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	pvcs, _ := client.CoreV1().PersistentVolumeClaims("agctl").List(ctx, metav1.ListOptions{})
	if len(pvcs.Items) != 0 {
		t.Fatalf("shared claim mode must not create per-user PVCs: %v", pvcs.Items)
	}
	pod, err := client.CoreV1().Pods("agctl").Get(ctx, resourceName(spec.RuntimeID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if claim := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; claim != "agctl-data" {
		t.Fatalf("pod volume claim = %q, want shared claim", claim)
	}
	mounts := pod.Spec.Containers[0].VolumeMounts
	if mounts[0].SubPath != "u17/work" || mounts[1].SubPath != "u17/dsh-home" {
		t.Fatalf("shared claim subpaths = %+v", mounts)
	}
	if init := pod.Spec.InitContainers[0].VolumeMounts[0]; init.SubPath != "u17" {
		t.Fatalf("init container must see the user directory of the shared claim: %+v", init)
	}
	if v, ok := envValue(pod.Spec.InitContainers[0], "AGCTL_DEFAULT_FULL_ACCESS"); !ok || v != "1" {
		t.Fatal("init container must receive AGCTL_DEFAULT_FULL_ACCESS for role user")
	}
	if v, ok := envValue(pod.Spec.Containers[0], "GITLAB_URL"); !ok || v != "https://gitlab.example.invalid" {
		t.Fatal("developer env must reach the dsh container")
	}
	if _, ok := envValue(pod.Spec.Containers[0], "broken"); ok {
		t.Fatal("malformed KEY=VALUE must be skipped")
	}
	if pod.Spec.Containers[0].LivenessProbe != nil {
		t.Fatal("liveness probe must be opt-in")
	}
	if pod.Annotations[annotationImage] != "reg.invalid/agctl-dsh:1" || pod.Labels[labelUserID] != "17" || pod.Labels[labelVersion] != "dev" {
		t.Fatalf("pod metadata: %v %v", pod.Annotations, pod.Labels)
	}
}

func TestSSHPublishesNodePortAndRunsSSHDCapableContainer(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", SSHHost: "nodes.example.invalid", Liveness: true}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	spec.SSHPubKey = "ssh-ed25519 AAAATEST dev@example"
	name := resourceName(spec.RuntimeID)
	// Фейковый API не выделяет nodePort — сервис создаём заранее с портом.
	_, err = client.CoreV1().Services("agctl").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "ssh-" + name, Namespace: "agctl"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{
			Name: "ssh", Port: 22, TargetPort: intstr.FromInt(2222), NodePort: 31022,
		}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if handle.SSHHost != "nodes.example.invalid" || handle.SSHPort != 31022 {
		t.Fatalf("handle SSH = %s:%d, want nodes.example.invalid:31022", handle.SSHHost, handle.SSHPort)
	}
	pod, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dsh := pod.Spec.Containers[0]
	if v, ok := envValue(dsh, "AGCTL_SSH_PUBKEY"); !ok || v != spec.SSHPubKey {
		t.Fatal("sshd public key must be passed to the container")
	}
	if len(dsh.Ports) != 2 || dsh.Ports[1].ContainerPort != 2222 {
		t.Fatalf("ssh container port missing: %+v", dsh.Ports)
	}
	if *pod.Spec.SecurityContext.RunAsNonRoot || *pod.Spec.SecurityContext.RunAsUser != 0 {
		t.Fatal("sshd pod must run as root with reduced capabilities")
	}
	if caps := dsh.SecurityContext.Capabilities; len(caps.Add) == 0 || caps.Drop[0] != "ALL" {
		t.Fatalf("expected drop ALL + explicit sshd capabilities: %+v", caps)
	}
	if dsh.LivenessProbe == nil || dsh.LivenessProbe.Exec == nil {
		t.Fatal("liveness probe requested but missing")
	}
	policy, err := client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "net-"+name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Spec.Ingress) != 2 || policy.Spec.Ingress[1].Ports[0].Port.IntValue() != 2222 {
		t.Fatalf("NetworkPolicy must open sshd when SSH is enabled: %+v", policy.Spec.Ingress)
	}
	if err := provider.StopChat(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("agctl").Get(ctx, "ssh-"+name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("StopChat must delete the NodePort SSH service")
	}

	// Без ключа SSH-сервис не создаётся, под non-root.
	spec.SSHPubKey = ""
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("agctl").Get(ctx, "ssh-"+name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("SSH service must not exist without a developer key")
	}
	pod, _ = client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
	if !*pod.Spec.SecurityContext.RunAsNonRoot {
		t.Fatal("pod without SSH must stay non-root")
	}
}

func TestGatewaySelectorRestrictsIngress(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", GatewaySelector: map[string]string{"app.kubernetes.io/name": "agctl"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	policy, err := client.NetworkingV1().NetworkPolicies("agctl").Get(ctx, "net-"+resourceName(spec.RuntimeID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	from := policy.Spec.Ingress[0].From[0].PodSelector.MatchLabels
	if from["app.kubernetes.io/name"] != "agctl" {
		t.Fatalf("ingress must be restricted to the gateway pods: %v", from)
	}
}

func TestWaitReadyReturnsAfterPodBecomesReady(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", WaitReady: true, ReadyTimeout: 20 * time.Second}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	name := resourceName(spec.RuntimeID)
	go func() {
		for {
			pod, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
			if err == nil {
				pod.Status.Phase = corev1.PodRunning
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "dsh", RestartCount: 2}}
				if _, err := client.CoreV1().Pods("agctl").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err == nil {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	start := time.Now()
	handle, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("EnsureChat did not return promptly after readiness")
	}
	if !strings.HasSuffix(handle.InstanceID, "-restart-2") {
		t.Fatalf("instance id must carry the restart count: %q", handle.InstanceID)
	}
}

func TestWaitReadyFailsFastOnTerminalPod(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", WaitReady: true, ReadyTimeout: 20 * time.Second}, client)
	if err != nil {
		t.Fatal(err)
	}
	spec := baseSpec()
	name := resourceName(spec.RuntimeID)
	go func() {
		for {
			pod, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
			if err == nil {
				pod.Status.Phase = corev1.PodFailed
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "dsh", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}}}
				if _, err := client.CoreV1().Pods("agctl").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err == nil {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	_, err = provider.EnsureChat(ctx, spec)
	if err == nil || !strings.Contains(err.Error(), "OOMKilled") {
		t.Fatalf("expected terminal pod error with reason, got %v", err)
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("terminal pod must be deleted for the retry")
	}
}

func TestVersionOrImageDriftReplacesPod(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	spec := baseSpec()
	name := resourceName(spec.RuntimeID)
	old, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", Version: "0.1.0"}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	current, err := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:2", Version: "0.2.0"}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := current.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Labels[labelVersion] != "0.2.0" || pod.Annotations[annotationImage] != "reg.invalid/agctl-dsh:2" {
		t.Fatalf("pod was not replaced on drift: %v %v", pod.Labels, pod.Annotations)
	}
	creates := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "pods" {
			creates++
		}
	}
	if creates != 2 {
		t.Fatalf("pod creates = %d, want 2 (original + replacement)", creates)
	}
}

func TestSweepVersionDriftDeletesOtherVersionsOnly(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	stale, _ := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", Version: "0.1.0"}, client)
	current, _ := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", Version: "0.2.0"}, client)
	staleSpec := baseSpec()
	freshSpec := baseSpec()
	freshSpec.RuntimeID, freshSpec.StorageID = "chat-abcd1234-u18", "u18"
	if _, err := stale.EnsureChat(ctx, staleSpec); err != nil {
		t.Fatal(err)
	}
	if _, err := current.EnsureChat(ctx, freshSpec); err != nil {
		t.Fatal(err)
	}
	if n := current.SweepVersionDrift(ctx); n != 1 {
		t.Fatalf("sweep removed %d runtimes, want 1", n)
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, resourceName(staleSpec.RuntimeID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("stale pod must be deleted")
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, resourceName(freshSpec.RuntimeID), metav1.GetOptions{}); err != nil {
		t.Fatal("current pod must survive the sweep")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("agctl").Get(ctx, "data-"+storageResourceName(staleSpec.StorageID), metav1.GetOptions{}); err != nil {
		t.Fatal("sweep must never delete user data")
	}
}

func TestHealthStatusClassifiesPods(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, _ := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1"}, client)
	spec := baseSpec()
	handle, err := provider.EnsureChat(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "healthy" {
		t.Fatalf("fresh pod = %q, want healthy", st)
	}
	name := resourceName(spec.RuntimeID)
	pod, _ := client.CoreV1().Pods("agctl").Get(ctx, name, metav1.GetOptions{})
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "dsh", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
	if _, err := client.CoreV1().Pods("agctl").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "unhealthy" {
		t.Fatalf("crash-looping pod = %q, want unhealthy", st)
	}
	if err := provider.StopChat(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if st, _ := provider.HealthStatus(ctx, handle.InstanceID); st != "unhealthy" {
		t.Fatalf("deleted pod = %q, want unhealthy", st)
	}
}

type fakeExec struct {
	out string
	err error
	cmd []string
}

func (f *fakeExec) Exec(_ context.Context, _, _, _ string, cmd []string) (string, error) {
	f.cmd = cmd
	return f.out, f.err
}

func TestHasActiveSSHUsesPodExec(t *testing.T) {
	ctx := context.Background()
	provider, _ := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1"}, fake.NewSimpleClientset())
	if active, err := provider.HasActiveSSH(ctx, agctlrt.ChatHandle{RuntimeID: "x"}); err != nil || active {
		t.Fatalf("without SSH port must be false without exec: %v %v", active, err)
	}
	exec := &fakeExec{out: "  PID COMMAND\n    1 /usr/sbin/sshd -D -e -p 2222 [listener]\n   42 sshd: root@pts/0\n"}
	provider.SetExecer(exec)
	active, err := provider.HasActiveSSH(ctx, agctlrt.ChatHandle{RuntimeID: "x", SSHPort: 31022})
	if err != nil || !active {
		t.Fatalf("active session expected: %v %v", active, err)
	}
	if strings.Join(exec.cmd, " ") != "ps -eo pid,args" {
		t.Fatalf("unexpected exec command %v", exec.cmd)
	}
	exec.out = "  PID COMMAND\n    1 /usr/sbin/sshd -D [listener]\n"
	if active, _ := provider.HasActiveSSH(ctx, agctlrt.ChatHandle{RuntimeID: "x", SSHPort: 31022}); active {
		t.Fatal("listener only must not count as an active session")
	}
}

func TestLegacyAdapterListsStopsAndRejectsStart(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	provider, _ := NewWithClient(Config{Namespace: "agctl", Image: "reg.invalid/agctl-dsh:1", SSHHost: "n.invalid"}, client)
	spec := baseSpec()
	spec.SSHPubKey = "ssh-ed25519 AAAA"
	if _, err := provider.EnsureChat(ctx, spec); err != nil {
		t.Fatal(err)
	}
	list, err := provider.ChatContainers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ChatContainers = %v, %v", list, err)
	}
	info := list[0]
	if info.UserID != 17 || info.Workspace != spec.RuntimeID || !info.SSH || info.ID != resourceName(spec.RuntimeID) {
		t.Fatalf("unexpected container info: %+v", info)
	}
	if _, err := provider.Start(ctx, provision.Layout{}, "ws", dockerrt.StartOpts{}, provision.Provider{}); err == nil {
		t.Fatal("legacy Start must be rejected")
	}
	if err := provider.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("agctl").Get(ctx, info.ID, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("Stop must delete the pod")
	}
	if err := provider.Stop(ctx, "agctl-chat-missing"); err != nil {
		t.Fatalf("stopping a missing pod must be a no-op: %v", err)
	}
}
