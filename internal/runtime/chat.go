// Package runtime defines runtime-neutral contracts shared by the Docker
// adapter and future Kubernetes runtime implementations.
package runtime

import (
	"context"

	"github.com/arijjia/agctl/internal/provision"
)

// ChatSpec is the stable logical identity of one user's lazy Chat runtime.
// StorageID survives runtime replacement and is unique within a provider's
// persistence scope (AGCTL_HOME for Docker, a dedicated namespace for K8s); it
// maps to a bind-mount or PVC. RuntimeID identifies compute, not storage/port.
type ChatSpec struct {
	RuntimeID    string
	StorageID    string
	UserID       int64
	Provider     provision.Provider
	SessionScope bool
	// SSHPubKey — роль developer: поднять sshd в контейнере (key-only,
	// порт публикуется только на loopback хоста). Пусто = sshd не поднимать.
	SSHPubKey string
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
