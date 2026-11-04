package dockerrt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// admin.go — инвентарь чат-контейнеров для админ-панели супер-админа
// (карточка admin-panel-super-admin). Только чтение: управление жизненным
// циклом остаётся у Runtime (Stop/Resume) и ensureChat сервера.

// ChatContainerInfo — одна строка списка контейнеров agctl (чаты).
type ChatContainerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"` // RuntimeID чата (метка agctl.workspace)
	UserID    int64  `json:"userId"`    // из суффикса RuntimeID "-u<N>"
	Image     string `json:"image"`
	ImageID   string `json:"imageId"`
	State     string `json:"state"`  // running | exited | paused | ...
	Status    string `json:"status"` // human-readable строка Docker
	CreatedAt int64  `json:"createdAt"`
	SSH       bool   `json:"ssh"` // опубликован ли порт 2222
}

// ChatContainers — все чат-контейнеры этого agctl (метка agctl.workspace),
// включая остановленные/уснувшие. Данные пользователей (тома) не трогаются.
func (rt *Runtime) ChatContainers(ctx context.Context) ([]ChatContainerInfo, error) {
	res, err := rt.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("dockerrt: container list: %w", err)
	}
	out := []ChatContainerInfo{}
	for _, c := range res.Items {
		ws := c.Labels[labelWorkspace]
		if ws == "" {
			continue // не наш контейнер
		}
		info := ChatContainerInfo{
			ID:        c.ID,
			Workspace: ws,
			Image:     c.Image,
			ImageID:   c.ImageID,
			State:     string(c.State),
			Status:    c.Status,
			CreatedAt: c.Created,
		}
		if len(c.Names) > 0 {
			info.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		// RuntimeID чата всегда "chat-<hash>-u<N>" (см. server.chatRuntimeID).
		if i := strings.LastIndex(ws, "-u"); i >= 0 {
			var uid int64
			if _, err := fmt.Sscanf(ws[i+2:], "%d", &uid); err == nil {
				info.UserID = uid
			}
		}
		for _, p := range c.Ports {
			if p.PrivatePort == 2222 { // порт КОНТЕЙНЕРА; PublicPort — хостовый
				info.SSH = true
				break
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// ContainerStatsSample — one-shot снимок потребления: память в байтах и CPU
// в процентах (по дельте к предыдущему образцу, как считает docker stats).
// Для уснувшего контейнера вернёт нули с ошибкой-причиной.
func (rt *Runtime) ContainerStatsSample(ctx context.Context, id string) (memBytes uint64, cpuPercent float64, err error) {
	res, err := rt.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("dockerrt: container stats %s: %w", id[:12], err)
	}
	defer res.Body.Close()
	var st container.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return 0, 0, fmt.Errorf("dockerrt: stats decode %s: %w", id[:12], err)
	}
	memBytes = st.MemoryStats.Usage
	pre := st.PreCPUStats.CPUUsage.TotalUsage
	cur := st.CPUStats.CPUUsage.TotalUsage
	sysPre, sysCur := st.PreCPUStats.SystemUsage, st.CPUStats.SystemUsage
	if pre > 0 && cur > pre && sysCur > sysPre && sysCur-sysPre > 0 {
		cpuDelta := float64(cur - pre)
		sysDelta := float64(sysCur - sysPre)
		cpus := float64(st.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = 1
		}
		cpuPercent = cpuDelta / sysDelta * cpus * 100
	}
	return memBytes, cpuPercent, nil
}
