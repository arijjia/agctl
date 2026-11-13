package axrt

import (
	"context"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/google/ax/pkg/apis/v1alpha1"
)

// legacy.go — адаптер server.RuntimeIface для AX: legacy workspace-места не
// поддерживаются, Stop/Logs и список задач нужны админке
// (/api/admin/containers); идентификатор «контейнера» — имя задачи.

// Start — legacy workspace: не поддерживается.
func (p *Provider) Start(context.Context, provision.Layout, string, dockerrt.StartOpts, provision.Provider) (dockerrt.StartResult, error) {
	return dockerrt.StartResult{}, ErrLegacyUnsupported
}

// Resume — legacy workspace: не поддерживается.
func (p *Provider) Resume(context.Context, string) error { return ErrLegacyUnsupported }

// Stop — усыпить задачу по имени (suspend; состояние сохраняется).
func (p *Provider) Stop(ctx context.Context, id string) error {
	_, err := p.ax.SuspendTask(ctx, &v1alpha1.SuspendTaskRequest{Atespace: p.cfg.Atespace, Name: taskNameOf(id)})
	if err != nil && !isNotFound(err) && !isAlreadySuspended(err) {
		return err
	}
	return nil
}

// Remove — удалить задачу целиком (админ: данные актора теряются).
func (p *Provider) Remove(ctx context.Context, id string) error {
	return p.DeleteTask(ctx, taskNameOf(id))
}

// Logs — хвост лога запуска dsh раннера задачи.
func (p *Provider) Logs(ctx context.Context, id, tail string) (string, error) {
	return p.bootLog(ctx, taskNameOf(id), tail)
}

// ChatContainers — задачи atespace в формате админки (containerLister).
func (p *Provider) ChatContainers(ctx context.Context) ([]dockerrt.ChatContainerInfo, error) {
	tasks, err := p.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dockerrt.ChatContainerInfo, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskInfo(task))
	}
	return out, nil
}

// taskInfo — сводка задачи для админки.
func taskInfo(task *v1alpha1.Task) dockerrt.ChatContainerInfo {
	fields := task.GetSpec().GetConfig().GetFields()
	userID, _ := strconv.ParseInt(fields[configKeyUserID].GetStringValue(), 10, 64)
	phase := task.GetStatus().GetPhase()
	state := strings.ToLower(phase)
	switch phase {
	case v1alpha1.PhaseRunning:
		if !taskReady(task) {
			state = "starting"
		}
	case v1alpha1.PhaseSuspended:
		state = "exited"
	}
	status := phase
	if msg := readyMessage(task); msg != "" && msg != phase {
		status = phase + ": " + msg
	}
	created := int64(0)
	if ts := task.GetMetadata().GetCreateTime(); ts != nil {
		created = ts.GetSeconds()
	}
	return dockerrt.ChatContainerInfo{
		ID: task.GetMetadata().GetName(), Name: task.GetMetadata().GetName(),
		Workspace: fields[configKeyRuntime].GetStringValue(), UserID: userID,
		Image: task.GetSpec().GetImage(), ImageID: task.GetSpec().GetImage(),
		State: state, Status: status, CreatedAt: created, SSH: false,
	}
}
