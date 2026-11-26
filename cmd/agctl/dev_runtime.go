package main

// dev_runtime.go — рантайм рабочего места локального режима: docker
// (контейнер, по умолчанию) или sbx (Docker Sandboxes, microVM). Выбор лежит
// в dev.yaml (runtime) и перечитывается на каждый старт рабочего места:
// смена в лаунчере или флагом --runtime действует со следующего перезапуска
// места, без перезапуска платформы.

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/cli"
	"github.com/arijjia/agctl/internal/localmode"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/sbxrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
)

// devRuntimeKind — рантайм из dev.yaml (нет файла или мусор — docker).
func devRuntimeKind(home string) string {
	cfg, err := localmode.Load(home)
	if err != nil {
		return localmode.RuntimeDocker
	}
	return cfg.RuntimeKind()
}

// devBackend — cli.Runtime локального режима: сервер на Docker с
// переключателем рантайма рабочих мест.
type devBackend struct{ home string }

func devRuntimeFlags(home string) cli.RuntimeFlags {
	return func(*flag.FlagSet, cli.UserConfig) cli.Runtime { return devBackend{home: home} }
}

func (d devBackend) Build(db *store.DB, sec *secrets.Manager, cfg server.Config) (*server.Server, error) {
	cfg.WrapChatProvider = func(docker agctlrt.ChatProvider) agctlrt.ChatProvider {
		return &sbxrt.Switch{
			Docker: docker,
			Kind:   func() string { return devRuntimeKind(d.home) },
			NewSbx: func(ctx context.Context) (*sbxrt.Runtime, error) {
				return sbxrt.New(ctx, sbxrt.Config{
					Home: cfg.Home, Image: cfg.Image,
					MemoryBytes: cfg.MemoryBytes, NanoCPUs: cfg.NanoCPUs,
					DshSkel: assets.DshSkel,
					// В sbx сеть запрещена по умолчанию всегда — белый список
					// действует и без egress_allowlist.
					Allow: cfg.EgressAllow,
				})
			},
		}
	}
	return server.New(db, sec, cfg)
}

func (d devBackend) StatusLine() string {
	return "docker: " + cli.DockerStatus() + "; рантайм рабочего места: " + devRuntimeKind(d.home)
}

// ── хуки лаунчера (server.LocalRuntimeHooks) ────────────────────────────────

// sbxProbe — проверка sbx с кэшем на полминуты: лаунчер опрашивает
// /api/local/ui часто, а проверка — это запуск CLI.
var sbxProbe struct {
	mu      sync.Mutex
	at      time.Time
	version string
	err     error
}

func probeSbx() (string, error) {
	sbxProbe.mu.Lock()
	defer sbxProbe.mu.Unlock()
	if sbxProbe.at.IsZero() || time.Since(sbxProbe.at) > 30*time.Second {
		sbxProbe.version, sbxProbe.err = sbxrt.Probe(context.Background(), "")
		sbxProbe.at = time.Now()
	}
	return sbxProbe.version, sbxProbe.err
}

func (h *devHooks) RuntimeKind() string { return devRuntimeKind(h.home) }

func (h *devHooks) Runtimes() []server.LocalRuntime {
	sbx := server.LocalRuntime{ID: localmode.RuntimeSbx, Title: "Docker Sandboxes (sbx, microVM)"}
	if version, err := probeSbx(); err != nil {
		sbx.Detail = err.Error()
	} else {
		sbx.Available, sbx.Detail = true, version
	}
	return []server.LocalRuntime{
		{ID: localmode.RuntimeDocker, Title: "Docker (контейнер)", Available: true},
		sbx,
	}
}

func (h *devHooks) SetRuntimeKind(kind string) error { return localmode.SetRuntime(h.home, kind) }
