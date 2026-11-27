package server

// local_images.go — раздел «Образы» лаунчера локального режима: каталог
// образов с тем, что внутри и какие места их используют, как каждый
// собирается (ступени, Dockerfile, аргументы, что качается из сети) и
// действия — скачать или собрать, пересобрать, удалить. Docker — через хуки
// хоста (LocalImagesHooks); долгие операции идут фоном одной задачей с
// журналом, открытые копии лаунчера узнают о ней из потока событий.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// LocalImageStage — ступень сборки образа: что добавляет, откуда Dockerfile,
// аргументы сборки, контекст и что качается из сети.
type LocalImageStage struct {
	Ref        string            `json:"ref"`
	Present    bool              `json:"present"`
	Kind       string            `json:"kind"`
	Title      string            `json:"title"`
	Source     string            `json:"source"`
	Dockerfile string            `json:"dockerfile"`
	Args       map[string]string `json:"args,omitempty"`
	Context    []string          `json:"context,omitempty"`
	From       []string          `json:"from,omitempty"`
	Apt        []string          `json:"apt,omitempty"`
	Npm        []string          `json:"npm,omitempty"`
	Downloads  []string          `json:"downloads,omitempty"`
}

// LocalImagePlan — как образ попадает на машину: сначала docker pull, при
// ответе реестра «нет такого» — сборка ступеней на этой машине.
type LocalImagePlan struct {
	Ref    string            `json:"ref"`
	Pull   string            `json:"pull"`
	Stages []LocalImageStage `json:"stages"`
}

// LocalImagesHooks — хост для раздела «Образы»: план сборки и удаление.
type LocalImagesHooks interface {
	ImagePlan(ref string) (LocalImagePlan, error)
	RemoveImage(ctx context.Context, ref string) error
}

func (s *Server) imagesHooks() (LocalImagesHooks, bool) {
	h, ok := s.localHooks().(LocalImagesHooks)
	return h, ok
}

// imageTask — фоновая операция раздела «Образы» (одна за раз).
func (s *Server) imageTask() *localTask {
	s.local.placeMu.Lock()
	defer s.local.placeMu.Unlock()
	if s.local.imageTask == nil {
		s.local.imageTask = &localTask{state: "idle"}
	}
	return s.local.imageTask
}

func (s *Server) registerLocalImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/ui/images", s.requireLocalOwner(s.handleImagesList))
	mux.HandleFunc("GET /api/local/ui/images/plan", s.requireLocalOwner(s.handleImagePlan))
	mux.HandleFunc("POST /api/local/ui/images/{action}", s.requireLocalOwner(s.handleImageAction))
}

// imageUse — место, которому назначен образ.
type imageUse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

// localImageView — образ каталога для раздела «Образы».
type localImageView struct {
	LocalImage
	Current bool       `json:"current"` // образ основного места по умолчанию
	UsedBy  []imageUse `json:"usedBy"`
}

// imageUsers — какие места используют образ ref.
func (s *Server) imageUsers(owner int64) map[string][]imageUse {
	out := map[string][]imageUse{}
	views, err := s.placeViews(owner)
	if err != nil {
		return out
	}
	for _, v := range views {
		out[v.Image] = append(out[v.Image], imageUse{ID: v.ID, Name: v.Name, Running: v.State == "running" || v.State == "starting"})
	}
	return out
}

func (s *Server) handleImagesList(w http.ResponseWriter, r *http.Request, id identity) {
	h := s.localHooks()
	users := s.imageUsers(id.ID)
	current := h.CurrentImage()
	seen := map[string]bool{}
	var out []localImageView
	for _, im := range h.Images() {
		seen[im.Ref] = true
		use := users[im.Ref]
		if use == nil {
			use = []imageUse{}
		}
		out = append(out, localImageView{LocalImage: im, Current: im.Ref == current, UsedBy: use})
	}
	// Образы, назначенные местам, но не из каталога (явный тег) — тоже видны.
	var extra []string
	for ref := range users {
		if ref != "" && !seen[ref] {
			extra = append(extra, ref)
		}
	}
	sort.Strings(extra)
	for _, ref := range extra {
		out = append(out, localImageView{LocalImage: LocalImage{Ref: ref, Note: "не из каталога профиля"}, UsedBy: users[ref]})
	}
	_, planOK := s.imagesHooks()
	writeJSON(w, http.StatusOK, map[string]any{"images": out, "task": s.imageTask().snapshot(), "actions": planOK})
}

func (s *Server) handleImagePlan(w http.ResponseWriter, r *http.Request, id identity) {
	h, ok := s.imagesHooks()
	if !ok {
		writeErr(w, http.StatusNotImplemented, "план сборки недоступен в этой сборке")
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if err := s.validImageRef(ref); err != nil || ref == "" {
		writeErr(w, http.StatusBadRequest, "нужен образ: ?ref=<образ>")
		return
	}
	plan, err := h.ImagePlan(ref)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	present := map[string]bool{}
	for _, im := range s.localHooks().Images() {
		present[im.Ref] = im.Present
	}
	for i := range plan.Stages {
		if !plan.Stages[i].Present {
			plan.Stages[i].Present = present[plan.Stages[i].Ref]
		}
	}
	writeJSON(w, http.StatusOK, plan)
}

type imageActionReq struct {
	Ref string `json:"ref"`
}

// handleImageAction — POST /api/local/ui/images/{ensure|rebuild|remove}
// {ref}: скачать или собрать недостающий; пересобрать (удалить тег и
// получить заново: из реестра или сборкой из кэша Docker); удалить.
// Образ работающего места не пересобирается и не удаляется.
func (s *Server) handleImageAction(w http.ResponseWriter, r *http.Request, id identity) {
	action := r.PathValue("action")
	var req imageActionReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if err := s.validImageRef(ref); err != nil || ref == "" {
		writeErr(w, http.StatusBadRequest, "нужен образ")
		return
	}
	hooks := s.localHooks()
	ih, ok := s.imagesHooks()
	if !ok && action != "ensure" {
		writeErr(w, http.StatusNotImplemented, "действие недоступно в этой сборке")
		return
	}
	if action != "ensure" && action != "rebuild" && action != "remove" {
		writeErr(w, http.StatusNotFound, "действия: ensure, rebuild, remove")
		return
	}
	var running []string
	for _, u := range s.imageUsers(id.ID)[ref] {
		if u.Running {
			running = append(running, "«"+u.Name+"»")
		}
	}
	if action != "ensure" && len(running) > 0 {
		writeErr(w, http.StatusConflict, "образ используют работающие места "+strings.Join(running, ", ")+" — сначала остановите их")
		return
	}
	t := s.imageTask()
	t.mu.Lock()
	if t.state == "building" {
		t.mu.Unlock()
		writeErr(w, http.StatusConflict, "уже идёт операция с образом "+t.image+" — дождитесь её")
		return
	}
	t.state, t.image, t.err, t.log = "building", ref, "", nil
	t.mu.Unlock()
	_ = s.db.Audit(id.Login, "local.image."+action, ref, "")
	s.local.events.publish("images")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
		defer cancel()
		err := s.runImageAction(ctx, action, ref, hooks, ih, t.logf)
		t.mu.Lock()
		if err != nil {
			t.state, t.err = "error", err.Error()
		} else {
			t.state = "idle"
		}
		t.mu.Unlock()
		if err != nil {
			t.logf("✗ " + err.Error())
		}
		s.local.events.publish("images")
		s.local.events.publish("places")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"task": t.snapshot()})
}

func (s *Server) runImageAction(ctx context.Context, action, ref string, hooks LocalHooks, ih LocalImagesHooks, logf func(string)) error {
	present := false
	for _, im := range hooks.Images() {
		present = present || (im.Ref == ref && im.Present)
	}
	switch action {
	case "remove":
		if !present {
			logf("образа " + ref + " на машине нет — удалять нечего")
			return nil
		}
		if err := ih.RemoveImage(ctx, ref); err != nil {
			return fmt.Errorf("удаление %s: %w", ref, err)
		}
		logf("✓ " + ref + " удалён (слои, общие с другими образами, остались)")
		return nil
	case "rebuild":
		if present {
			if err := ih.RemoveImage(ctx, ref); err != nil {
				return fmt.Errorf("пересборка: удаление прежнего %s: %w", ref, err)
			}
			logf("прежний " + ref + " удалён — получаю заново (реестр или сборка из кэша Docker)")
		}
	case "ensure":
		if present {
			logf(ref + " уже есть на машине")
			return nil
		}
	default:
		return errors.New("неизвестное действие")
	}
	if err := hooks.EnsureImage(ctx, ref, logf); err != nil {
		return err
	}
	logf("✓ " + ref + " готов")
	return nil
}
