package k8srt

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// spdyExecer — pods/exec через SPDY-исполнитель client-go (прод; требует
// RBAC-право create на pods/exec).
type spdyExecer struct {
	restConfig *rest.Config
	client     kubernetes.Interface
}

func (e *spdyExecer) Exec(ctx context.Context, namespace, pod, container string, cmd []string) (string, error) {
	req := e.client.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container, Command: cmd, Stdout: true, Stderr: true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("k8srt: exec executor: %w", err)
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", fmt.Errorf("k8srt: exec %v in %s/%s: %w (%s)", cmd, pod, container, err, stderr.String())
	}
	return stdout.String(), nil
}
