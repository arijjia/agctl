package main

// axegress.go — сборка axrt.EgressAutomation для режима ax с
// --ax-inject-llm-key=0: gRPC-клиент Control API Substrate (ate-api-server)
// и писатель k8s Secret с ключами провайдеров (in-cluster service account).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/arijjia/agctl/internal/runtime/axrt"
	"go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// axEgressAutomation — клиенты egress-инъекции: ate-api-server + k8s API.
func axEgressAutomation(ateAPI, cluster, kubeconfig string) (*axrt.EgressAutomation, error) {
	conn, err := grpc.NewClient(ateAPI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("ax egress: connect ate-api-server %s: %w", ateAPI, err)
	}
	var restConfig *rest.Config
	if kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ax egress: load Kubernetes credentials: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ax egress: create Kubernetes client: %w", err)
	}
	ns := axEgressNamespace()
	if ns == "" {
		_ = conn.Close()
		return nil, errors.New("ax egress: namespace шлюза неизвестна (запустите agctl в кластере или задайте kubeconfig)")
	}
	return &axrt.EgressAutomation{
		Control:   ateapi.NewControlClient(conn),
		Secrets:   &k8sSecretWriter{client: client},
		Cluster:   cluster,
		Namespace: ns,
	}, nil
}

// axEgressNamespace — namespace пода шлюза из projected service-account
// token (пусто вне кластера); Secret'ы egress-инъекции живут рядом.
func axEgressNamespace() string {
	raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// k8sSecretWriter — axrt.SecretWriter поверх client-go: create при
// отсутствии, update при расхождении data.
type k8sSecretWriter struct {
	client kubernetes.Interface
}

// EnsureSecret — idempotent sync Opaque Secret.
func (w *k8sSecretWriter) EnsureSecret(ctx context.Context, namespace, name string, data map[string][]byte) error {
	api := w.client.CoreV1().Secrets(namespace)
	secret, err := api.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = api.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{"app.kubernetes.io/name": "agctl", "app.kubernetes.io/component": "llm-egress"},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if secretsEqual(secret.Data, data) {
		return nil
	}
	secret.Data = data
	_, err = api.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// secretsEqual — data Secret'ов совпадает (ключи и значения).
func secretsEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || string(v) != string(bv) {
			return false
		}
	}
	return true
}
