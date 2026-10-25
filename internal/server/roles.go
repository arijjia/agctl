package server

// roles.go — роль developer (v3 unified runtime): заявка → апрув админом →
// привязка к учётке (users.role = 'developer'). Роль открывает: sshd в
// персональном контейнере, создание dev-мест (legacy), git-контролы (волна C).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/store"
)

type roleRequestReq struct {
	Motivation string `json:"motivation"`
}

// handleRoleRequestCreate — пользователь подаёт заявку на роль developer.
func (s *Server) handleRoleRequestCreate(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "developer" || id.Role == "admin" {
		writeErr(w, http.StatusBadRequest, "роль developer уже есть")
		return
	}
	if pending, err := s.db.PendingRoleRequestExists(id.ID); err == nil && pending {
		writeErr(w, http.StatusConflict, "заявка уже отправлена и ждёт апрува администратора")
		return
	}
	var req roleRequestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	motivation := strings.TrimSpace(req.Motivation)
	if len(motivation) > 2000 {
		writeErr(w, http.StatusBadRequest, "мотивация: максимум 2000 символов")
		return
	}
	rid, err := s.db.CreateRoleRequest(id.ID, motivation)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "role-request", id.Login, "заявка на роль developer")
	writeJSON(w, http.StatusOK, map[string]any{
		"request": map[string]any{"id": rid, "status": "pending"},
	})
}

// handleRoleRequestsList — свои заявки; админ видит все (с логинами).
func (s *Server) handleRoleRequestsList(w http.ResponseWriter, r *http.Request, id identity) {
	var (
		requests []store.RoleRequest
		err      error
	)
	if id.Role == "admin" {
		requests, err = s.db.RoleRequestsAll()
	} else {
		requests, err = s.db.RoleRequestsForUser(id.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(requests))
	for _, r := range requests {
		out = append(out, map[string]any{
			"id": r.ID, "userId": r.UserID, "login": r.Login, "status": r.Status,
			"motivation": r.Motivation, "decidedBy": r.DecidedBy,
			"decidedAt": r.DecidedAt, "createdAt": r.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": id.Role, "requests": out})
}

// handleRoleRequestDecision — апрув/отклон заявки админом. Апрув привязывает
// роль developer к учётке заявителя (постоянно).
func (s *Server) handleRoleRequestDecision(approve bool) func(http.ResponseWriter, *http.Request, identity) {
	return func(w http.ResponseWriter, r *http.Request, id identity) {
		s.requestMu.Lock()
		defer s.requestMu.Unlock()

		rid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "неверный id заявки")
			return
		}
		status := "rejected"
		if approve {
			status = "approved"
		}
		req, err := s.db.RoleRequestByID(rid)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "заявка не найдена")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if req.Status != "pending" {
			writeErr(w, http.StatusConflict, "заявка уже решена (статус: "+req.Status+")")
			return
		}
		userID, err := s.db.DecideRoleRequest(rid, status, id.Login)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if approve {
			if err := s.db.UpdateUserRole(userID, "developer"); err != nil {
				writeErr(w, http.StatusInternalServerError, "не удалось назначить роль: "+err.Error())
				return
			}
		}
		_ = s.db.Audit(id.Login, "role-request-"+status, req.LoginOrID(userID), "role=developer привязана к учётке")
		writeJSON(w, http.StatusOK, map[string]any{"request": map[string]any{"id": rid, "status": status}})
	}
}

// ── SSH-ключ разработчика в персональном контейнере ─────────────────────────

type meSSHReq struct {
	PubKey string `json:"pubKey"`
}

// handleMeSSHPut — разработчик кладёт свой публичный SSH-ключ; контейнер
// поднимет sshd при следующем старте (key-only, порт на loopback).
func (s *Server) handleMeSSHPut(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role != "developer" && id.Role != "admin" {
		writeErr(w, http.StatusForbidden, "SSH-канал — только для роли developer (подайте заявку)")
		return
	}
	var req meSSHReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	key, err := parseSSHPublicKey(req.PubKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if key == "" {
		writeErr(w, http.StatusBadRequest, "пустой ключ")
		return
	}
	if err := s.db.SettingSet(fmt.Sprintf("dev-ssh-pubkey-u%d", id.ID), key); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "ssh-key", id.Login, "pubkey сохранён")
	port := s.chatSSHPort(id.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"sshPort": port,
		"note":    "вступит в силу при следующем старте контейнера (сейчас: " + strconv.Itoa(port) + ")",
	})
}

// handleMeSSHGet — публичный ключ и живой SSH-порт (0 = контейнер спит).
func (s *Server) handleMeSSHGet(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role != "developer" && id.Role != "admin" {
		writeErr(w, http.StatusForbidden, "SSH-канал — только для роли developer")
		return
	}
	key, _ := s.db.SettingGet(fmt.Sprintf("dev-ssh-pubkey-u%d", id.ID))
	writeJSON(w, http.StatusOK, map[string]any{
		"pubKey":  key,
		"sshPort": s.chatSSHPort(id.ID),
	})
}

// chatSSHPort — SSH-порт живого персонального контейнера (0 = спит/нет).
func (s *Server) chatSSHPort(userID int64) int {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if rt, ok := s.chats[userID]; ok {
		return rt.handle.SSHPort
	}
	return 0
}
