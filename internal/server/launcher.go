package server

// launcher.go — лаунчер роли developer (карточка developer-launcher-images):
// после авторизации developer не получает контейнер автоматически; он выбирает
// образ из каталога (Nexus организации и пр.), управляет своими контейнерами
// (старт/стоп/рестарт). Роль user ведёт себя как раньше: ленивый дефолтный
// чат. Админу личный контейнер не выдаётся вовсе (см. auditedChat/rootEntry).

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/runtime/dockerrt"
)

const chatImageSettingPrefix = "chat-image-u"

func chatImageSettingKey(userID int64) string {
	return chatImageSettingPrefix + strconv.FormatInt(userID, 10)
}

// developerChatImage — желаемый образ рантайма пользователя; "" = дефолт.
func (s *Server) developerChatImage(userID int64) string {
	v, err := s.db.SettingGet(chatImageSettingKey(userID))
	if err != nil || v == "" {
		return ""
	}
	return v
}

// launcherCatalog — каталог образов: cfg.DevImages, а если он не задан —
// единственный дефолтный образ.
func (s *Server) launcherCatalog() []string {
	if len(s.cfg.DevImages) > 0 {
		return s.cfg.DevImages
	}
	return []string{s.cfg.Image}
}

// handleLauncherImages — GET /api/launcher/images: каталог образов сервера
// и текущий выбор пользователя.
func (s *Server) handleLauncherImages(w http.ResponseWriter, r *http.Request, id identity) {
	writeJSON(w, http.StatusOK, map[string]any{
		"images":  s.launcherCatalog(),
		"default": s.cfg.Image,
		"current": s.developerChatImage(id.ID),
	})
}

// imagePutReq — тело PUT /api/me/image.
type imagePutReq struct {
	Image string `json:"image"`
}

// handleMeImagePut — PUT /api/me/image: выбрать образ для следующего старта
// рантайма. Образ обязан быть из каталога сервера (проверка на старте всё
// равно дублируется: желаемый образ без локальной копии тянется из реестра).
func (s *Server) handleMeImagePut(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	var req imagePutReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Image = strings.TrimSpace(req.Image)
	if req.Image == "" {
		// пустое значение = сброс на дефолтный образ
		if err := s.db.SettingSet(chatImageSettingKey(id.ID), ""); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"image": ""})
		return
	}
	allowed := false
	for _, img := range s.launcherCatalog() {
		if img == req.Image {
			allowed = true
			break
		}
	}
	if !allowed {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("образ %q вне каталога сервера (--dev-images)", req.Image))
		return
	}
	if err := s.db.SettingSet(chatImageSettingKey(id.ID), req.Image); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "launcher.image.set", req.Image, "")
	writeJSON(w, http.StatusOK, map[string]any{"image": req.Image})
}

// handleMeRuntimeStart — POST /api/me/runtime/start: поднять свой runtime
// (ensureChat идемпотентен; для живого рантайма просто вернёт handle).
func (s *Server) handleMeRuntimeStart(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	rt, err := s.ensureChat(id.ID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "старт рантайма: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "running", "endpoint": rt.handle.Endpoint, "sshPort": rt.handle.SSHPort, "sshHost": rt.handle.SSHHost,
	})
}

// handleMeRuntimeStop — POST /api/me/runtime/stop: усыпить свой runtime
// немедленно (compute удаляется, том остаётся; идемпотентно для спящего).
func (s *Server) handleMeRuntimeStop(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	s.chatMu.Lock()
	rt := s.chats[id.ID]
	s.chatMu.Unlock()
	if rt == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
		return
	}
	if err := s.chatProvider.StopChat(r.Context(), rt.handle); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.evictChatByUser(id.ID)
	_ = s.db.Audit(id.Login, "launcher.runtime.stop", rt.handle.RuntimeID, "том сохранён")
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
}

// maxLauncherLogTail — верхняя граница хвоста логов за один запрос
// (санитизация всё равно проходит по всей строке).
const maxLauncherLogTail = 2000

// handleMeRuntimeLogs — GET /api/me/runtime/logs?tail=&container=: хвост
// логов своего чат-контейнера (лаунчер developer'а). Контейнер ищется по
// списку agctl-чатов (чужие имена не адресуемы); логи прогоняются через
// redactWorkspaceLogs, как и legacy /api/workspaces/{id}/logs.
func (s *Server) handleMeRuntimeLogs(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	list, ok := s.listChatContainers()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "docker-рантайм недоступен")
		return
	}
	want := r.URL.Query().Get("container")
	var own []dockerrt.ChatContainerInfo
	for _, c := range list {
		if c.UserID == id.ID {
			own = append(own, c)
		}
	}
	if len(own) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"logs": "(контейнера нет — нажмите «Запустить»)"})
		return
	}
	var target *dockerrt.ChatContainerInfo
	for i := range own {
		if own[i].Name == want {
			target = &own[i]
			break
		}
	}
	if target == nil {
		target = &own[0]
	}
	tail := atoiDefault(r.URL.Query().Get("tail"), 200)
	if tail > maxLauncherLogTail {
		tail = maxLauncherLogTail
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	logs, err := s.rt.Logs(ctx, target.ID, strconv.Itoa(tail))
	if err != nil {
		logs = "(контейнер не отвечает: " + err.Error() + ")"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"container": target.Name,
		"state":     target.State,
		"logs":      redactWorkspaceLogs(logs),
	})
}

// handleMeContainers — GET /api/me/containers: свои контейнеры (лаунчер).
func (s *Server) handleMeContainers(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	list, ok := s.listChatContainers()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "docker-рантайм недоступен")
		return
	}
	own := []map[string]any{}
	for _, c := range list {
		if c.UserID != id.ID {
			continue
		}
		own = append(own, map[string]any{
			"name": c.Name, "workspace": c.Workspace, "image": c.Image,
			"state": c.State, "status": c.Status, "ssh": c.SSH,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": own})
}
