package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/provision"
)

// handleProjectDelete — DELETE /api/projects/{name}: удалить воркспейс
// вместе с каталогом на диске (данные проекта стираются безвозвратно;
// подтверждение — на клиенте). Сессионные записи dsh (том) не трогаются:
// история чатов сохраняется, воркспейс исчезает из списка.
func (s *Server) handleProjectDelete(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("name")
	if _, code, msg := s.resolveProjectDir(id.ID, name); code != 0 && code != http.StatusNotFound {
		writeErr(w, code, msg)
		return
	}
	err := provision.DeleteProject(s.cfg.Home, id.ID, name)
	switch {
	case err == nil:
	case errors.Is(err, provision.ErrInvalidProject):
		writeErr(w, http.StatusBadRequest, "некорректное имя проекта")
		return
	case os.IsNotExist(err):
		// Уже нет (повторное удаление) — идемпотентный успех.
	default:
		log.Printf("[projects] u%d delete %q failed", id.ID, name)
		writeErr(w, http.StatusInternalServerError, "не удалось удалить каталог воркспейса")
		return
	}
	_ = s.db.Audit(id.Login, "project.delete", name, "каталог удалён с диска")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "project": name})
}

// handleProjectsList — GET /api/projects: реальные каталоги-проекты
// пользователя (без скрытых и secret-путей).
func (s *Server) handleProjectsList(w http.ResponseWriter, r *http.Request, id identity) {
	projects, err := provision.ListProjects(s.cfg.Home, id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать проекты")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// handleProjectCreate — POST /api/projects: создать проект (пустой каталог
// или клон публичного GitHub — только developer); тело ≤8 KiB и строго один
// JSON-объект. packages — id платформенного каталога, ставятся сразу после
// создания (тот же механизм, что «Возможности» в консоли воркспейсов).
func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request, id identity) {
	var req struct {
		Name     string   `json:"name"`
		CloneUrl string   `json:"cloneUrl"`
		Packages []string `json:"packages"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON проекта")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "ожидался один JSON объект")
		return
	}
	if len(req.Packages) > 16 {
		writeErr(w, http.StatusBadRequest, "слишком много пакетов")
		return
	}
	for _, pkgID := range req.Packages {
		if _, err := provision.FindCatalogPackage(s.brand.FS, pkgID); err != nil {
			writeErr(w, http.StatusBadRequest, "неизвестный пакет: "+err.Error())
			return
		}
	}
	clone := strings.TrimSpace(req.CloneUrl) != ""
	// Квота тома (карточка chat-prewarm-pool, план 3): новый проект не
	// создаётся, если том пользователя уже упёрся в лимит. Обход файлов —
	// редкая операция создания, не на горячем пути.
	if q := s.userVolumeQuotaGB(id.ID); q > 0 {
		if used := userVolumeUsage(s.cfg.Home, id.ID); used >= int64(q*1e9) {
			_ = s.db.Audit(id.Login, "project.create.quota", req.Name, "")
			writeErr(w, http.StatusForbidden, fmt.Sprintf(
				"квота тома исчерпана (%.1f ГБ); освободите место или попросите администратора поднять лимит", q))
			return
		}
	}
	// Клонирование по URL — developer-настройка: это сетевой запрос с сервера,
	// обычному режиму user он не нужен (карточка dev-workspace-clone-url).
	var (
		project provision.Project
		err     error
	)
	if clone {
		if id.Role != "developer" && id.Role != "admin" {
			writeErr(w, http.StatusForbidden, "клонирование репозитория по URL доступно роли developer")
			return
		}
		project, err = provision.CloneProject(s.cfg.Home, id.ID, req.Name, req.CloneUrl)
	} else {
		project, err = provision.CreateProject(s.cfg.Home, id.ID, req.Name)
	}
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidProject):
			writeErr(w, http.StatusBadRequest, "недопустимое имя проекта; клон — только публичный HTTPS URL github.com/owner/repository")
		case errors.Is(err, provision.ErrProjectExists):
			writeErr(w, http.StatusConflict, "воркспейс уже существует: на диске есть непустой каталог с этим именем; старый можно удалить в «Воркспейсы: возможности»")
		default:
			log.Printf("[projects] u%d create failed (clone=%t)", id.ID, clone)
			if clone {
				writeErr(w, http.StatusBadGateway, "не удалось клонировать публичный GitHub-репозиторий")
			} else {
				writeErr(w, http.StatusInternalServerError, "не удалось создать воркспейс")
			}
		}
		return
	}
	// Пакеты каталога («возможности воркспейса») — сразу после создания.
	// Ошибка установки не сносит воркспейс: сообщаем в ответе списком.
	hostDir, dirErr := provision.ProjectDirectory(s.cfg.Home, id.ID, project.Name)
	failed := []string{}
	if dirErr != nil {
		log.Printf("[projects] u%d %s: каталог воркспейса не разрешён", id.ID, project.Name)
		failed = append(failed, req.Packages...)
	} else {
		for _, pkgID := range req.Packages {
			pkg, err := provision.FindCatalogPackage(s.brand.FS, pkgID)
			if err == nil {
				err = provision.InstallCatalogPackage(hostDir, s.brand.FS, pkg)
			}
			if err != nil {
				log.Printf("[projects] u%d %s: пакет %s не установлен", id.ID, project.Name, pkgID)
				failed = append(failed, pkgID)
			}
		}
	}
	if len(failed) > 0 {
		_ = s.db.Audit(id.Login, "project.create.package-failed", project.Name, strings.Join(failed, ","))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": project, "packagesFailed": failed})
}

// autosaveLocalProjects runs for every user's personal volume, including sleeping
// chats. It never provisions a missing volume or adopts non-repository folders.
// Повторы сбойного проекта идут с backoff: после autosaveFailThreshold подряд
// неудач — разовый repair-pass и экспоненциальная пауза (до autosaveBackoffCap),
// чтобы «уже сломанный» проект не молотил лог каждые 2 минуты.
func (s *Server) autosaveLocalProjects() {
	users, err := s.db.UsersList()
	if err != nil {
		return
	}
	for _, user := range users {
		projects, err := provision.ListProjects(s.cfg.Home, user.ID)
		if err != nil {
			log.Printf("[project-autosave] u%d: cannot list projects", user.ID)
			continue
		}
		for _, project := range projects {
			key := autosaveKey(user.ID, project.Name)
			if !s.autosaveDue(key) {
				continue
			}
			_, err := provision.AutosaveLocalProject(s.cfg.Home, user.ID, project.Name)
			switch {
			case err == nil || os.IsNotExist(err):
				s.autosaveSucceeded(key)
			case s.autosaveFailed(key, err):
				// Avoid file paths/content in logs; autosave is invisible to chat UI.
				log.Printf("[project-autosave] u%d %s: local save failed (%s), backing off",
					user.ID, project.Name, autosaveFailReason(err))
				if provision.AutosaveRepairPass(s.cfg.Home, user.ID, project.Name, autosaveStaleLockAge) {
					log.Printf("[project-autosave] u%d %s: repair pass removed stale index.lock", user.ID, project.Name)
				}
			default:
				// Повторная неудача в backoff-окне — молча, причина уже в логе.
			}
		}
	}
}

const (
	autosaveFailThreshold = 3                // подряд неудач до repair-pass и backoff
	autosaveBackoffCap    = time.Hour        // потолок экспоненциальной паузы
	autosaveStaleLockAge  = 10 * time.Minute // index.lock старше — протухший
)

// autosaveFailState — счётчик подряд идущих неудач autosave одного проекта.
type autosaveFailState struct {
	fails int
	next  time.Time // раньше этой попытки не стартуем
}

func autosaveKey(userID int64, name string) string {
	return fmt.Sprintf("%d/%s", userID, name)
}

// autosaveDue — true, если проект не в backoff-паузе.
func (s *Server) autosaveDue(key string) bool {
	s.autosaveMu.Lock()
	defer s.autosaveMu.Unlock()
	st, ok := s.autosaveFails[key]
	return !ok || !time.Now().Before(st.next)
}

// autosaveSucceeded обнуляет серию неудач.
func (s *Server) autosaveSucceeded(key string) {
	s.autosaveMu.Lock()
	defer s.autosaveMu.Unlock()
	delete(s.autosaveFails, key)
}

// autosaveFailed наращивает серию и возвращает true, только если пора
// реагировать (порог достигнут или пауза истекла): в этот момент вызывающий
// делает repair-pass и пишет причину в лог.
func (s *Server) autosaveFailed(key string, err error) bool {
	s.autosaveMu.Lock()
	defer s.autosaveMu.Unlock()
	if s.autosaveFails == nil {
		s.autosaveFails = map[string]autosaveFailState{}
	}
	st := s.autosaveFails[key]
	st.fails++
	if st.fails < autosaveFailThreshold {
		s.autosaveFails[key] = st
		return st.fails == 1 // первая неудача серии — сразу в лог
	}
	// Порог и далее: экспоненциальная пауза 1x, 2x, 4x… периода, с потолком.
	step := s.cfg.AutosaveEvery << (st.fails - autosaveFailThreshold)
	if step <= 0 || step > autosaveBackoffCap {
		step = autosaveBackoffCap
	}
	st.next = time.Now().Add(step)
	s.autosaveFails[key] = st
	return true
}

// autosaveFailReason — короткая причина сбоя для лога: без путей и контента.
func autosaveFailReason(err error) string {
	switch {
	case errors.Is(err, provision.ErrInvalidProject):
		return "invalid tree"
	case os.IsNotExist(err):
		return "missing"
	case os.IsTimeout(err), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case strings.Contains(err.Error(), "index.lock"):
		return "index locked"
	default:
		return "git error"
	}
}
