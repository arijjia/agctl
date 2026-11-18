package server

import (
	"context"
	"net/http"
	"time"
)

// Module — подключаемая часть сервера: свои маршруты и фоновые задачи.
//
// Ядро (Routes / StartBackground) регистрирует только общее для всех режимов:
// вход, /api/me, проекты, каталог, MCP, /chat/, карту, рипер и сверку
// рантаймов. Всё, что нужно только серверу организации (регистрация, роли,
// лаунчер developer'а, админка, prewarm, autosave, legacy workspaces, Jira,
// конфиг раннера AX), — модули; их подключает точка входа через
// Config.Modules. Локальный режим (agctl dev) модулей не подключает, поэтому
// их маршрутов в нём нет, а их код не попадает в граф вызовов локального
// бинаря.
type Module struct {
	Name       string
	Routes     func(s *Server, mux *http.ServeMux)
	Background func(ctx context.Context, s *Server)
}

// ServerModules — полный набор модулей сервера организации (agctl serve).
func ServerModules() []Module {
	return []Module{
		RegistrationModule(),
		RolesModule(),
		LauncherModule(),
		AdminModule(),
		PrewarmModule(),
		AutosaveModule(),
		LegacyWorkspacesModule(),
		JiraModule(),
		AxProviderConfigModule(),
	}
}

// hasModule сообщает, подключён ли модуль с таким именем.
func (s *Server) hasModule(name string) bool {
	for _, m := range s.cfg.Modules {
		if m.Name == name {
			return true
		}
	}
	return false
}

// RegistrationModule — самостоятельная регистрация учётных записей
// (первый аккаунт — admin, лимит регистраций с IP).
func RegistrationModule() Module {
	return Module{Name: "registration", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("POST /api/register", s.handleRegister)
	}}
}

// RolesModule — заявки на роль developer и SSH-ключ developer'а.
func RolesModule() Module {
	return Module{Name: "roles", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("POST /api/role-requests", s.requireUser(s.handleRoleRequestCreate))
		mux.HandleFunc("GET /api/role-requests", s.requireUser(s.handleRoleRequestsList))
		mux.HandleFunc("POST /api/role-requests/{id}/approve", s.requireAdmin(s.handleRoleRequestDecision(true)))
		mux.HandleFunc("POST /api/role-requests/{id}/reject", s.requireAdmin(s.handleRoleRequestDecision(false)))
		mux.HandleFunc("GET /api/me/ssh", s.requireUser(s.handleMeSSHGet))
		mux.HandleFunc("PUT /api/me/ssh", s.requireUser(s.handleMeSSHPut))
	}}
}

// LauncherModule — лаунчер developer'а: образы, старт/стоп/логи своего
// контейнера (карточка developer-launcher-images).
func LauncherModule() Module {
	return Module{Name: "launcher", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/me/runtime", s.requireUser(s.handleMyRuntimeGet))
		mux.HandleFunc("GET /api/launcher/images", s.requireDeveloper(s.handleLauncherImages))
		mux.HandleFunc("PUT /api/me/image", s.requireDeveloper(s.handleMeImagePut))
		mux.HandleFunc("POST /api/me/runtime/start", s.requireDeveloper(s.handleMeRuntimeStart))
		mux.HandleFunc("POST /api/me/runtime/stop", s.requireDeveloper(s.handleMeRuntimeStop))
		mux.HandleFunc("GET /api/me/containers", s.requireDeveloper(s.handleMeContainers))
		mux.HandleFunc("GET /api/me/runtime/logs", s.requireDeveloper(s.handleMeRuntimeLogs))
		// Слоты мульти-рантайма (карточка developer-multi-containers).
		mux.HandleFunc("GET /api/me/slots", s.requireDeveloper(s.handleMeSlots))
		mux.HandleFunc("PUT /api/me/slots/{slot}/image", s.requireDeveloper(s.handleMeSlotImagePut))
		mux.HandleFunc("POST /api/me/slots/{slot}/start", s.requireDeveloper(s.handleMeSlotStart))
		mux.HandleFunc("POST /api/me/slots/{slot}/stop", s.requireDeveloper(s.handleMeSlotStop))
		mux.HandleFunc("POST /api/me/slots/{slot}/activate", s.requireDeveloper(s.handleMeSlotActivate))
		if s.cfg.Tier0 && s.webFS != nil {
			mux.HandleFunc("GET /launcher", s.adminPageHandler(s.webFS))
		}
	}}
}

// AdminModule — админ-панель: пользователи, аудит, контейнеры, квоты,
// журнал шлюза, траектории агента (карточка admin-panel-super-admin).
func AdminModule() Module {
	return Module{Name: "admin", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminUsers))
		mux.HandleFunc("GET /api/admin/audit", s.requireAdmin(s.handleAdminAudit))
		mux.HandleFunc("GET /api/admin/overview", s.requireAdmin(s.handleAdminOverview))
		mux.HandleFunc("GET /api/admin/users/details", s.requireAdmin(s.handleAdminUsersDetailed))
		mux.HandleFunc("PUT /api/admin/users/{id}/limits", s.requireAdmin(s.handleAdminUserLimitsPut))
		mux.HandleFunc("GET /api/admin/containers", s.requireAdmin(s.handleAdminContainers))
		mux.HandleFunc("POST /api/admin/containers/{name}/stop", s.requireAdmin(s.handleAdminContainerStop))
		mux.HandleFunc("POST /api/admin/containers/{name}/start", s.requireAdmin(s.handleAdminContainerStart))
		mux.HandleFunc("POST /api/admin/containers/{name}/restart", s.requireAdmin(s.handleAdminContainerRestart))
		mux.HandleFunc("GET /api/admin/containers/{name}/logs", s.requireAdmin(s.handleAdminContainerLogs))
		mux.HandleFunc("GET /api/admin/requests", s.requireAdmin(s.handleAdminRequests))
		mux.HandleFunc("GET /api/admin/activity", s.requireAdmin(s.handleAdminActivity))
		mux.HandleFunc("GET /api/admin/quotas", s.requireAdmin(s.handleAdminQuotasList))
		mux.HandleFunc("PUT /api/admin/quotas", s.requireAdmin(s.handleAdminQuotaPut))
		mux.HandleFunc("DELETE /api/admin/quotas/{id}", s.requireAdmin(s.handleAdminQuotaDelete))
		mux.HandleFunc("GET /api/admin/users/{id}/sessions", s.requireAdmin(s.handleAdminUserSessions))
		// «Вся траектория» одним списком (без ввода id пользователя).
		mux.HandleFunc("GET /api/admin/sessions", s.requireAdmin(s.handleAdminSessions))
		mux.HandleFunc("GET /api/admin/users/{id}/sessions/{workspace}/{session}", s.requireAdmin(s.handleAdminUserSessionTrace))
		if s.cfg.Tier0 && s.webFS != nil {
			// Тот же SPA-бандл, роль определяет рендер на клиенте.
			mux.HandleFunc("GET /admin", s.adminPageHandler(s.webFS))
		}
	}}
}

// PrewarmModule — prewarm-пул: N последних активных пользователей держатся
// тёплыми (login-хук — в handleLogin, он смотрит на этот модуль).
func PrewarmModule() Module {
	return Module{Name: "prewarm", Background: func(ctx context.Context, s *Server) {
		if !s.prewarmEnabled() {
			return
		}
		s.every(ctx, s.cfg.ChatPrewarmEvery, func() { s.prewarmOnce(nil) })
	}}
}

// AutosaveModule — go-git autosave проектов пользователей локальным
// коммитом без push. Локальному режиму не подходит: /workspace там —
// настоящие репозитории разработчика.
func AutosaveModule() Module {
	return Module{Name: "autosave", Background: func(ctx context.Context, s *Server) {
		s.every(ctx, s.cfg.AutosaveEvery, func() {
			s.autosaveAll()
			s.autosaveLocalProjects()
		})
	}}
}

// LegacyWorkspacesModule — workspace-места до v3: API /api/workspaces,
// заявки, шаблоны и комплекты, шлюз /w/{id}/ (создание отвечает 410).
// Удаление — карточка legacy-workspace-code-cleanup.
func LegacyWorkspacesModule() Module {
	return Module{Name: "legacy-workspaces", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/templates", s.requireUser(s.handleTemplates))
		mux.HandleFunc("GET /api/kits", s.requireUser(s.handleKits))
		mux.HandleFunc("GET /api/workspaces", s.requireUser(s.handleWorkspacesList))
		mux.HandleFunc("POST /api/workspaces", s.requireUser(s.handleWorkspaceCreate))
		mux.HandleFunc("GET /api/workspaces/{id}", s.requireUser(s.handleWorkspaceGet))
		mux.HandleFunc("POST /api/workspaces/{id}/start", s.requireUser(s.handleWorkspaceStart))
		mux.HandleFunc("POST /api/workspaces/{id}/stop", s.requireUser(s.handleWorkspaceStop))
		mux.HandleFunc("DELETE /api/workspaces/{id}", s.requireUser(s.handleWorkspaceDelete))
		mux.HandleFunc("GET /api/workspaces/{id}/logs", s.requireUser(s.handleWorkspaceLogs))
		mux.HandleFunc("PATCH /api/workspaces/{id}/provider", s.requireUser(s.handleWorkspaceProvider))
		mux.HandleFunc("PATCH /api/workspaces/{id}/ssh", s.requireUser(s.handleWorkspaceSSH))
		mux.HandleFunc("POST /api/workspace-requests", s.requireUser(s.handleWorkspaceRequestCreate))
		mux.HandleFunc("GET /api/workspace-requests", s.requireUser(s.handleWorkspaceRequestsList))
		mux.HandleFunc("POST /api/workspace-requests/{id}/approve", s.requireAdmin(s.handleWorkspaceRequestDecision(true)))
		mux.HandleFunc("POST /api/workspace-requests/{id}/reject", s.requireAdmin(s.handleWorkspaceRequestDecision(false)))
		mux.HandleFunc("GET /w/{id}", s.requireUser(s.workspaceProxyEntry))
		// webmanifest браузер забирает без учётных данных agctl — публично
		// только этот нечувствительный статический документ.
		mux.HandleFunc("GET /w/{id}/manifest.webmanifest", s.workspaceManifest)
		// DSH Web UI: GET — документы/ассеты, WebSocket и SSE; POST — RPC
		// (/api/*). Оба метода должны доходить до обратного прокси.
		mux.HandleFunc("GET /w/{id}/", s.requireUser(s.workspaceProxy))
		mux.HandleFunc("POST /w/{id}/", s.requireUser(s.workspaceProxy))
	}}
}

// JiraModule — поллер Jira (скелет, включается env AGCTL_JIRA_*).
func JiraModule() Module {
	return Module{Name: "jira", Background: func(ctx context.Context, s *Server) {
		s.StartJiraPoller(ctx)
	}}
}

// AxProviderConfigModule — fetch-at-boot конфига провайдера раннером AX
// (бэкенд ax, ProviderAtBoot): env задачи конфиг не содержит.
func AxProviderConfigModule() Module {
	return Module{Name: "ax-provider-config", Routes: func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /ax/provider-config/{task}", s.handleAxProviderConfig)
	}}
}

// every вызывает fn с периодом d до отмены ctx.
func every(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// every — лидер-гейтованный вариант every: тик выполняется только репликой-
// лидером (карточка ha-control-plane); без lease (single-реплика) — всегда.
func (s *Server) every(ctx context.Context, d time.Duration, fn func()) {
	every(ctx, d, func() {
		if s.isLeader() {
			fn()
		}
	})
}
