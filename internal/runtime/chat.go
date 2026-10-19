// Package runtime defines runtime-neutral contracts shared by the Docker
// adapter and future Kubernetes runtime implementations.
package runtime

import (
	"context"

	"github.com/arijjia/agctl/internal/provision"
)

// ChatSpec is the stable logical identity of one user's lazy Chat runtime.
// StorageID survives runtime replacement and maps to a Docker bind-mount or a
// Kubernetes PVC; RuntimeID identifies the compute resource, not a host port.
type ChatSpec struct {
	RuntimeID    string
	StorageID    string
	UserID       int64
	Provider     provision.Provider
	SessionScope bool
}

// ChatHandle describes one running runtime generation. Endpoint is private to
// the agctl gateway (Docker loopback now, Kubernetes Service DNS in a K8s
// adapter); InstanceID identifies the current container/Pod generation.
type ChatHandle struct {
	RuntimeID  string
	InstanceID string
	Endpoint   string
}

// ChatProvider manages scale-to-zero Chat compute while keeping StorageID's
// persistent data independent of the runtime. EnsureChat must be idempotent
// by RuntimeID (important for K8s create races/restarts); StopChat deletes only
// compute, never the data volume. LogsChat provides the DSH boot token source.
type ChatProvider interface {
	EnsureChat(ctx context.Context, spec ChatSpec) (ChatHandle, error)
	StopChat(ctx context.Context, handle ChatHandle) error
	LogsChat(ctx context.Context, handle ChatHandle, tail string) (string, error)
}
