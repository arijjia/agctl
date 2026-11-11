package k8srt

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	corev1 "k8s.io/api/core/v1"
)

// legacy.go — адаптер server.RuntimeIface для Kubernetes: legacy
// workspace-места (Start/Resume) в кластере не поддерживаются (их API и так
// отвечает 410), а Stop/Remove/Logs и список рантаймов нужны админке
// (/api/admin/containers): идентификатор «контейнера» — имя пода.

// ErrLegacyUnsupported — legacy workspace-рантайм недоступен в Kubernetes.
var ErrLegacyUnsupported = fmt.Errorf("k8srt: legacy workspace runtime is not supported on Kubernetes")

// Start — legacy workspace: не поддерживается.
func (p *Provider) Start(context.Context, provision.Layout, string, dockerrt.StartOpts, provision.Provider) (dockerrt.StartResult, error) {
	return dockerrt.StartResult{}, ErrLegacyUnsupported
}

// Resume — legacy workspace: не поддерживается.
func (p *Provider) Resume(context.Context, string) error { return ErrLegacyUnsupported }

// Stop — усыпить рантайм по имени пода (compute удаляется, том остаётся).
func (p *Provider) Stop(ctx context.Context, id string) error {
	pod, err := p.findPod(ctx, id)
	if err != nil {
		return err
	}
	if pod == nil {
		return nil
	}
	return p.deleteRuntime(ctx, pod.Name)
}

// Remove — то же, что Stop: данные пользователя адаптер не удаляет никогда.
func (p *Provider) Remove(ctx context.Context, id string) error { return p.Stop(ctx, id) }

// Logs — хвост логов dsh по имени пода/поколению.
func (p *Provider) Logs(ctx context.Context, id, tail string) (string, error) {
	pod, err := p.findPod(ctx, id)
	if err != nil {
		return "", err
	}
	if pod == nil {
		return "", fmt.Errorf("k8srt: Pod %s not found", id)
	}
	return p.podLogs(ctx, pod.Name, tail)
}

// ChatContainers — чат-поды установки в формате админки (containerLister).
func (p *Provider) ChatContainers(ctx context.Context) ([]dockerrt.ChatContainerInfo, error) {
	pods, err := p.ListRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dockerrt.ChatContainerInfo, 0, len(pods))
	for i := range pods {
		out = append(out, podInfo(&pods[i]))
	}
	return out, nil
}

// podInfo — сводка пода для админки.
func podInfo(pod *corev1.Pod) dockerrt.ChatContainerInfo {
	userID, _ := strconv.ParseInt(pod.Labels[labelUserID], 10, 64)
	image := pod.Annotations[annotationImage]
	imageID := ""
	ssh := false
	for _, c := range pod.Spec.Containers {
		if c.Name == "dsh" {
			if image == "" {
				image = c.Image
			}
			for _, port := range c.Ports {
				if port.Name == "ssh" {
					ssh = true
				}
			}
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "dsh" {
			imageID = cs.ImageID
		}
	}
	state := strings.ToLower(string(pod.Status.Phase))
	if podReady(pod) {
		state = "running"
	} else if pod.Status.Phase == corev1.PodRunning {
		state = "starting"
	}
	status := podWaitingReason(pod)
	if podHealth(pod) == "unhealthy" {
		status = "unhealthy: " + status
	}
	return dockerrt.ChatContainerInfo{
		ID: pod.Name, Name: pod.Name,
		Workspace: pod.Labels[labelRuntimeID], UserID: userID,
		Image: image, ImageID: imageID,
		State: state, Status: status,
		CreatedAt: pod.CreationTimestamp.Unix(), SSH: ssh,
	}
}
