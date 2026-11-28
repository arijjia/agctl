package server

// launcher.go — лаунчер роли developer (карточка developer-launcher-images):
// после авторизации developer не получает контейнер автоматически; он выбирает
// образ из каталога (Nexus организации и пр.), управляет своими контейнерами
// (старт/стоп/рестарт). Роль user ведёт себя как раньше: ленивый дефолтный
// чат. Админу личный контейнер не выдаётся вовсе (см. auditedChat/rootEntry).
//
// Мульти-рантайм (карточка developer-multi-containers): developer держит
// несколько живых контейнеров с разными образами — слоты 0..LauncherSlotsMax-1.
// Слот 0 — исторический основной (том u<ID> без суффикса), слот N — том
// u<N>s<N>; активный слот открывается по /chat/ и виден проектным API шлюза.
// Лимит живых слотов — cfg.LauncherSlotsMax (роль user всегда один слот).

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

// chatImageSettingKey — образ слота: слот 0 хранит исторический ключ
// (совместимость с существующими установками), слот N — "-s<N>".
func chatImageSettingKey(userID int64, slot int) string {
	if slot <= 0 {
		return chatImageSettingPrefix + strconv.FormatInt(userID, 10)
	}
	return chatImageSettingPrefix + strconv.FormatInt(userID, 10) + "-s" + strconv.Itoa(slot)
}

// chatActiveSlotKey — активный слот пользователя (settings KV, 0 = основной).
const chatActiveSlotPrefix = "chat-active-slot-u"

func chatActiveSlotKey(userID int64) string {
	return chatActiveSlotPrefix + strconv.FormatInt(userID, 10)
}

// activeChatSlot — слот, который откроется по /chat/ (роль user — всегда 0).
func (s *Server) activeChatSlot(userID int64, role string) int {
	if role != "developer" {
		return 0
	}
	raw, err := s.db.SettingGet(chatActiveSlotKey(userID))
	if err != nil {
		return 0
	}
	slot, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || slot < 0 || slot >= s.slotLimit() {
		return 0
	}
	return slot
}

// slotLimit — лимит слотов developer'а на установку (--launcher-slots-max).
// Локальный режим по умолчанию — потолок maxChatSlots: машина владельца, все
// слоты — его рабочие места (основное + дополнительные лаунчера v2).
func (s *Server) slotLimit() int {
	if s.cfg.LauncherSlotsMax > 0 && s.cfg.LauncherSlotsMax <= maxChatSlots {
		return s.cfg.LauncherSlotsMax
	}
	if s.isLocal() {
		return maxChatSlots
	}
	return 3
}

// developerChatImage — желаемый образ слота; "" = дефолт.
func (s *Server) developerChatImage(userID int64, slot int) string {
	v, err := s.db.SettingGet(chatImageSettingKey(userID, slot))
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
		"current": s.developerChatImage(id.ID, 0),
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
		if err := s.db.SettingSet(chatImageSettingKey(id.ID, 0), ""); err != nil {
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
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, 0), req.Image); err != nil {
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
	rt, err := s.startChat(id.ID, 0)
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
	// Флаг «остановлено владельцем» — только у кого есть лаунчер, чтобы
	// запустить место снова; роль user будит рантайм открытием чата.
	had, err := s.stopChatKey(chatKey{id.ID, 0}, id.Role == "developer", "владельцем")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !had {
		writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
		return
	}
	_ = s.db.Audit(id.Login, "launcher.runtime.stop", s.chatRuntimeID(id.ID, 0), "том сохранён")
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
}

// slotPathValue — номер слота из пути (0..max-1); роль user — только слот 0,
// админ — отказ везде (личный runtime не выдаётся).
func (s *Server) slotPathValue(r *http.Request, id identity) (int, bool) {
	if id.Role == "admin" {
		return 0, false
	}
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || slot < 0 || slot >= s.slotLimit() {
		return 0, false
	}
	if id.Role != "developer" && slot != 0 {
		return 0, false
	}
	return slot, true
}

// slotStateReq — тело POST /api/me/slots/{slot}/start|stop|activate.
type slotAction string

// liveSlotsOfUser — число живых слотов пользователя: кэш шлюза + контейнеры
// докера (кэш теряется при рестарте серва, контейнеры — нет).
func (s *Server) liveSlotsOfUser(userID int64) int {
	s.chatMu.Lock()
	live := map[int]bool{}
	for k := range s.chats {
		if k.uid == userID {
			live[k.slot] = true
		}
	}
	s.chatMu.Unlock()
	if list, ok := s.listChatContainers(); ok {
		for _, c := range list {
			if c.UserID == userID && c.State == "running" {
				live[c.Slot] = true
			}
		}
	}
	return len(live)
}

// handleMeSlots — GET /api/me/slots: срез слотов лаунчера developer'а.
func (s *Server) handleMeSlots(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		writeErr(w, http.StatusForbidden, "личный runtime админу не выдаётся")
		return
	}
	containers, dockerOK := s.listChatContainers()
	bySlot := map[int]dockerrt.ChatContainerInfo{}
	if dockerOK {
		for _, c := range containers {
			if c.UserID == id.ID {
				bySlot[c.Slot] = c
			}
		}
	}
	s.chatMu.Lock()
	live := map[int]bool{}
	for k := range s.chats {
		if k.uid == id.ID {
			live[k.slot] = true
		}
	}
	s.chatMu.Unlock()
	active := s.activeChatSlot(id.ID, id.Role)
	slots := []map[string]any{}
	for slot := 0; slot < s.slotLimit(); slot++ {
		if id.Role != "developer" && slot != 0 {
			break // роль user: только основной слот
		}
		entry := map[string]any{
			"slot": slot, "image": s.developerChatImage(id.ID, slot), "active": slot == active,
		}
		if c, ok := bySlot[slot]; ok {
			entry["state"] = c.State
			entry["container"] = c.Name
			entry["status"] = c.Status
			entry["runningImage"] = c.Image
		} else if live[slot] {
			// Docker-листер недоступен (не docker-бэкенд): состояние из кэша
			// шлюза — старт был через ensureChat и не может быть здесь stale.
			entry["state"] = "running"
		} else {
			entry["state"] = "absent"
		}
		slots = append(slots, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slots": slots, "max": s.slotLimit(), "live": s.liveSlotsOfUser(id.ID),
	})
}

// handleMeSlotImagePut — PUT /api/me/slots/{slot}/image: образ слота.
func (s *Server) handleMeSlotImagePut(w http.ResponseWriter, r *http.Request, id identity) {
	slot, ok := s.slotPathValue(r, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "слот недоступен этой роли")
		return
	}
	var req imagePutReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Image = strings.TrimSpace(req.Image)
	if req.Image != "" {
		allowed := false
		for _, img := range s.launcherCatalog() {
			if img == req.Image {
				allowed = true
				break
			}
		}
		if !allowed {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("образ %q вне каталога сервера (--dev-images)", req.Image))
			return
		}
	}
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, slot), req.Image); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "launcher.slot.image", strconv.Itoa(slot), req.Image)
	writeJSON(w, http.StatusOK, map[string]any{"slot": slot, "image": req.Image})
}

// handleMeSlotStart — POST /api/me/slots/{slot}/start.
func (s *Server) handleMeSlotStart(w http.ResponseWriter, r *http.Request, id identity) {
	slot, ok := s.slotPathValue(r, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "слот недоступен этой роли")
		return
	}
	if slot > 0 && s.liveSlotsOfUser(id.ID) >= s.slotLimit() {
		writeErr(w, http.StatusConflict, fmt.Sprintf("лимит живых слотов (%d) исчерпан — усыпите один из них", s.slotLimit()))
		return
	}
	rt, err := s.startChat(id.ID, slot)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "старт рантайма: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "launcher.slot.start", strconv.Itoa(slot), rt.handle.RuntimeID)
	writeJSON(w, http.StatusOK, map[string]any{
		"slot": slot, "status": "running", "endpoint": rt.handle.Endpoint,
		"sshPort": rt.handle.SSHPort, "sshHost": rt.handle.SSHHost,
	})
}

// handleMeSlotStop — POST /api/me/slots/{slot}/stop.
func (s *Server) handleMeSlotStop(w http.ResponseWriter, r *http.Request, id identity) {
	slot, ok := s.slotPathValue(r, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "слот недоступен этой роли")
		return
	}
	had, err := s.stopChatKey(chatKey{id.ID, slot}, true, "владельцем")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !had {
		writeJSON(w, http.StatusOK, map[string]any{"slot": slot, "status": "sleeping"})
		return
	}
	_ = s.db.Audit(id.Login, "launcher.slot.stop", strconv.Itoa(slot), s.chatRuntimeID(id.ID, slot))
	writeJSON(w, http.StatusOK, map[string]any{"slot": slot, "status": "sleeping"})
}

// handleMeSlotActivate — POST /api/me/slots/{slot}/activate: слот, который
// откроется по /chat/ и чей workspace видят проектные API шлюза.
func (s *Server) handleMeSlotActivate(w http.ResponseWriter, r *http.Request, id identity) {
	slot, ok := s.slotPathValue(r, id)
	if !ok {
		writeErr(w, http.StatusForbidden, "слот недоступен этой роли")
		return
	}
	if err := s.db.SettingSet(chatActiveSlotKey(id.ID), strconv.Itoa(slot)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "launcher.slot.activate", strconv.Itoa(slot), "")
	writeJSON(w, http.StatusOK, map[string]any{"slot": slot, "active": true})
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
			"state": c.State, "status": c.Status, "ssh": c.SSH, "slot": c.Slot,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": own})
}
