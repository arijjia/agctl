package server

// admin_sessions.go — «траектории агента» для админ-панели (карточка
// admin-panel-super-admin): чтение сессий dsh из персонального тома
// $AGCTL_HOME/chats/u<id>/dsh-home/sessions. Сессии — zstd-сжатый JSONL
// (session.v4.jsonl.zstd). Читаем только sessions/: .credentials.yaml и
// прочие секретные файлы тома этими маршрутами не отдаются.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// SessionInfo — одна сессия пользователя: каталог
// sessions/<workspaceKey>/session-<uuid>.
type SessionInfo struct {
	Workspace string `json:"workspace"` // ключ каталога (напр. "--workspace--")
	ID        string `json:"id"`        // имя каталога сессии
	SizeBytes int64  `json:"sizeBytes"`
	Modified  string `json:"modified"`
	HasTrace  bool   `json:"hasTrace"` // есть session.v4.jsonl.zstd
}

// SessionRowAll — сессия в общем списке админки: + владелец.
type SessionRowAll struct {
	SessionInfo
	UserID int64 `json:"userId"`
}

const (
	traceFileName = "session.v4.jsonl.zstd"
	// Границы ответа: не отдавать больше этих значений, чтобы админ-запрос
	// не стал веерной нагрузкой на диск.
	maxEvents     = 800
	maxTraceBytes = 8 << 20 // 8 МиБ распакованного JSONL на запрос
)

// validSessionPart — защита от path traversal: части пути сессии разрешены
// только из безопасного алфавита (workspace-ключи dsh и имена session-<uuid>
// состоят из них же).
func validSessionPart(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return !strings.Contains(s, "..")
}

func userSessionsRoot(home string, userID int64) string {
	return filepath.Join(home, "chats", fmt.Sprintf("u%d", userID), "dsh-home", "sessions")
}

// handleAdminUserSessions — GET /api/admin/users/{id}/sessions.
func (s *Server) handleAdminUserSessions(w http.ResponseWriter, r *http.Request, _ identity) {
	userID, err := pathInt64(r, "id")
	if err != nil || userID <= 0 {
		writeErr(w, http.StatusBadRequest, "неверный id пользователя")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.userSessionInfos(userID)})
}

// userSessionInfos — сессии одного пользователя из его персонального тома.
// Том пустой/не создан — пустой список.
func (s *Server) userSessionInfos(userID int64) []SessionInfo {
	root := userSessionsRoot(s.cfg.Home, userID)
	entries, err := os.ReadDir(root)
	if err != nil {
		// Нет сессий (том пуст или runtime ещё не поднимался) — пустой список.
		return []SessionInfo{}
	}
	out := []SessionInfo{}
	for _, ws := range entries {
		if !ws.IsDir() || !validSessionPart(ws.Name()) {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, ws.Name()))
		if err != nil {
			continue
		}
		for _, se := range sessions {
			if !se.IsDir() || !validSessionPart(se.Name()) || !strings.HasPrefix(se.Name(), "session-") {
				continue
			}
			info := SessionInfo{Workspace: ws.Name(), ID: se.Name()}
			if st, err := se.Info(); err == nil {
				info.Modified = st.ModTime().UTC().Format(time.RFC3339)
			}
			// Размер и наличие трейса считаем по файлам внутри (их немного).
			files, err := os.ReadDir(filepath.Join(root, ws.Name(), se.Name()))
			if err != nil {
				continue
			}
			for _, f := range files {
				if st, err := f.Info(); err == nil {
					info.SizeBytes += st.Size()
				}
				if f.Name() == traceFileName {
					info.HasTrace = true
				}
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Modified > out[j].Modified
	})
	return out
}

// maxAllSessions — верхняя граница общего списка сессий: админ-запрос не
// должен стать веерной нагрузкой на диск.
const maxAllSessions = 1000

// handleAdminSessions — GET /api/admin/sessions: сессии ВСЕХ пользователей
// одним списком (вкладка «Траектории»: «вся траектория» без ввода id).
// Отсортировано по свежести, ограничено maxAllSessions.
func (s *Server) handleAdminSessions(w http.ResponseWriter, r *http.Request, _ identity) {
	chats := filepath.Join(s.cfg.Home, "chats")
	users, err := os.ReadDir(chats)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []SessionRowAll{}})
		return
	}
	out := []SessionRowAll{}
	for _, u := range users {
		if !u.IsDir() || !strings.HasPrefix(u.Name(), "u") {
			continue
		}
		uid, err := strconv.ParseInt(strings.TrimPrefix(u.Name(), "u"), 10, 64)
		if err != nil || uid <= 0 {
			continue
		}
		for _, info := range s.userSessionInfos(uid) {
			out = append(out, SessionRowAll{SessionInfo: info, UserID: uid})
		}
		if len(out) >= maxAllSessions {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	if len(out) > maxAllSessions {
		out = out[:maxAllSessions]
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// handleAdminUserSessionTrace — GET
// /api/admin/users/{id}/sessions/{workspace}/{session}: распакованный JSONL
// сессии как массив событий (ограничен maxEvents/maxTraceBytes).
func (s *Server) handleAdminUserSessionTrace(w http.ResponseWriter, r *http.Request, _ identity) {
	userID, err := pathInt64(r, "id")
	if err != nil || userID <= 0 {
		writeErr(w, http.StatusBadRequest, "неверный id пользователя")
		return
	}
	ws, se := r.PathValue("workspace"), r.PathValue("session")
	if !validSessionPart(ws) || !validSessionPart(se) || !strings.HasPrefix(se, "session-") {
		writeErr(w, http.StatusBadRequest, "неверный путь сессии")
		return
	}
	path := filepath.Join(userSessionsRoot(s.cfg.Home, userID), ws, se, traceFileName)
	data, err := os.ReadFile(path) // #nosec G304 — путь собран из валидированных частей
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "трейс сессии не найден")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "zstd: "+err.Error())
		return
	}
	defer dec.Close()
	raw, err := dec.DecodeAll(data, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "zstd decode: "+err.Error())
		return
	}
	if len(raw) > maxTraceBytes {
		raw = raw[:maxTraceBytes]
	}
	events := []json.RawMessage{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(events) >= maxEvents {
			break
		}
		if json.Valid([]byte(line)) {
			events = append(events, json.RawMessage(line))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace": ws, "session": se, "events": events,
		"truncated": len(events) >= maxEvents,
	})
}

// userVolumeUsage — суммарный размер тома пользователя (байты) для списка
// пользователей админки. Считается по файлам; тома маленькие, но вызов
// делаем только по запросу (?withSizes=1).
func userVolumeUsage(home string, userID int64) int64 {
	var total int64
	root := filepath.Join(home, "chats", fmt.Sprintf("u%d", userID))
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if st, err := d.Info(); err == nil {
				total += st.Size()
			}
		}
		return nil
	})
	return total
}
