// Package runtime defines runtime-neutral contracts shared by the Docker
// adapter and future Kubernetes runtime implementations.
package runtime

import (
	"context"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

// ChatSpec is the stable logical identity of one user's lazy Chat runtime.
// StorageID survives runtime replacement and is unique within a provider's
// persistence scope (AGCTL_HOME for Docker, a dedicated namespace for K8s); it
// maps to a bind-mount or PVC. RuntimeID identifies compute, not storage/port.
type ChatSpec struct {
	RuntimeID string
	StorageID string
	UserID    int64
	Provider  provision.Provider
	// SSHPubKey — роль developer: поднять sshd в контейнере (key-only,
	// порт публикуется только на loopback хоста). Пусто = sshd не поднимать.
	SSHPubKey string
	// Image — желаемый образ рантайма (лаунчер developer'а, карточка
	// developer-launcher-images). Пусто = образ по умолчанию провайдера.
	Image string
	// DefaultFullAccess — роль user: пресет разрешений danger-full-access
	// назначается дефолтом свежих сессий (пресет-селектор user не показывается).
	DefaultFullAccess bool
	// EgressAllowlist — контейнер на выделенной сети, egress только DNS
	// (карточка egress-allowlist; opt-in флаг serve).
	EgressAllowlist bool
	// DevEnv — переменные окружения контейнера (KEY=VALUE) для ролей
	// developer/admin: креды доменных скриптов (GitLab/Jira) и оверрайды LLM
	// из хостового .env. Роль user их не получает.
	DevEnv []string
}

// ChatHandle describes one running runtime generation. Endpoint is private to
// the agctl gateway (Docker loopback now, Kubernetes Service DNS in a K8s
// adapter); InstanceID identifies the current container/Pod generation.
type ChatHandle struct {
	RuntimeID  string
	InstanceID string
	Endpoint   string
	// SSHPort — хост-порт SSH канала (роль developer); 0 = sshd не поднимался.
	SSHPort int
	// SSHHost — адрес SSH-канала для пользователя: пусто = loopback хоста agctl
	// (Docker); Kubernetes-адаптер отдаёт внешний адрес узла/балансировщика
	// (NodePort).
	SSHHost string
	// Headers — заголовки, которые шлюз добавляет к каждому запросу в Endpoint
	// (например, ate-target-actor маршрутизатора Agent Substrate: один общий
	// Endpoint, актор выбирается заголовком). nil = ничего не добавлять.
	Headers map[string]string
}

// ChatProvider manages scale-to-zero Chat compute while keeping StorageID's
// persistent data independent of the runtime. EnsureChat must be idempotent
// by RuntimeID (important for K8s create races/restarts); StopChat deletes only
// compute, never the data volume. LogsChat provides the DSH boot token source.
type ChatProvider interface {
	EnsureChat(ctx context.Context, spec ChatSpec) (ChatHandle, error)
	StopChat(ctx context.Context, handle ChatHandle) error
	LogsChat(ctx context.Context, handle ChatHandle, tail string) (string, error)
	// HasActiveSSH reports a live authenticated/connecting sshd child process.
	// Implementations without SSH can always return false.
	HasActiveSSH(ctx context.Context, handle ChatHandle) (bool, error)
}

// ActiveSSHSession — есть ли среди процессов рантайма живой sshd-сеанс
// (не listener). Вход — вывод `ps -eo pid,args` (или аналогичный список
// строк: PID и полная команда); используется Docker- и Kubernetes-адаптерами
// для продления lease developer'а перед TTL-усыплением.
func ActiveSSHSession(ps string) bool {
	for _, line := range strings.Split(ps, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		cmd := strings.ToLower(strings.Join(fields[1:], " "))
		if strings.HasPrefix(cmd, "sshd:") && !strings.Contains(cmd, "[listener]") {
			return true
		}
	}
	return false
}
