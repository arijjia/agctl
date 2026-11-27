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
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LocalImageStage — ступень сборки образа: что добавляет, откуда Dockerfile,
// аргументы сборки, контекст и что качается из сети.
type LocalImageStage struct {
	Ref        string              `json:"ref"`
	Present    bool                `json:"present"`
	Kind       string              `json:"kind"`
	Title      string              `json:"title"`
	Source     string              `json:"source"`
	Dockerfile string              `json:"dockerfile"`
	Args       map[string]string   `json:"args,omitempty"`
	Context    []LocalImageContext `json:"context,omitempty"`
	From       []string            `json:"from,omitempty"`
	Apt        []string            `json:"apt,omitempty"`
	Npm        []string            `json:"npm,omitempty"`
	Downloads  []string            `json:"downloads,omitempty"`
}

// LocalImageContext — файл или каталог контекста сборки ступени, кроме
// Dockerfile: откуда берётся (встроен в бинарь agctl, скелет dsh-home,
// guest-бинарь на диске, файл профиля), размер, у каталога — число файлов.
type LocalImageContext struct {
	Name     string `json:"name"`              // путь в контексте: entrypoint.sh, skel/, agctl
	Dir      bool   `json:"dir,omitempty"`     // каталог
	Origin   string `json:"origin"`            // embedded | skel | guest | profile
	From     string `json:"from"`              // откуда — для человека
	Path     string `json:"path,omitempty"`    // в исходниках agctl, в профиле или на диске
	Size     int64  `json:"size"`              // байт; у каталога — всех файлов
	Files    int    `json:"files,omitempty"`   // число файлов каталога
	Missing  bool   `json:"missing,omitempty"` // guest-бинаря нет на диске
	Modified string `json:"modified,omitempty"`
	Note     string `json:"note,omitempty"`
}

// LocalImageFile — файл контекста сборки (текст) или каталог (все файлы с
// путями от корня контекста).
type LocalImageFile struct {
	Path  string               `json:"path"`
	Size  int64                `json:"size"`
	Dir   bool                 `json:"dir,omitempty"`
	Files []LocalImageFileItem `json:"files,omitempty"`
	Text  string               `json:"text,omitempty"`
}

// LocalImageFileItem — файл каталога контекста.
type LocalImageFileItem struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// LocalImagePlan — как образ попадает на машину: сначала docker pull из
// реестра (Pull — откуда именно и что дальше, PullRef — что тянется), не
// вышло — сборка ступеней на этой машине.
type LocalImagePlan struct {
	Ref     string            `json:"ref"`
	Pull    string            `json:"pull"`
	PullRef string            `json:"pullRef,omitempty"`
	Stages  []LocalImageStage `json:"stages"`
}

// LocalImagesHooks — хост для раздела «Образы»: план сборки, файлы
// контекста, скачать из реестра, пересобрать и удалить.
type LocalImagesHooks interface {
	ImagePlan(ref string) (LocalImagePlan, error)
	// ImageFile — файл или каталог rel контекста сборки ступени stage (с 1,
	// как в плане): текст до 256 КБ или список файлов. Нет такого пути —
	// ошибка с fs.ErrNotExist; двоичный или большой файл — ошибка с размером.
	ImageFile(ref string, stage int, rel string) (LocalImageFile, error)
	// PullFrom — откуда «Скачать из реестра» тянет ref ("" — реестр
	// выключен в профиле); у образов каталога — уже в LocalImage.PullFrom.
	PullFrom(ref string) string
	// PullImage — «Скачать из реестра»: docker pull (семейство agctl-dsh* —
	// из реестра образов agctl под локальное имя), без сборки. updated —
	// образ на машине сменился (новая версия или его не было).
	PullImage(ctx context.Context, ref string, logf func(string)) (updated bool, err error)
	// BuildImage — «Пересобрать»: сборка на этой машине, без реестра.
	BuildImage(ctx context.Context, ref string, logf func(string)) error
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
	mux.HandleFunc("GET /api/local/ui/images/file", s.requireLocalOwner(s.handleImageFile))
	mux.HandleFunc("POST /api/local/ui/images/{action}", s.requireLocalOwner(s.handleImageAction))
}

// imageUse — место, которому назначен образ.
type imageUse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	slot    int
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
		out[v.Image] = append(out[v.Image], imageUse{ID: v.ID, Name: v.Name, Running: v.State == "running" || v.State == "starting", slot: v.Slot})
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
	ih, planOK := s.imagesHooks()
	for _, ref := range extra {
		im := LocalImage{Ref: ref, Note: "не из каталога профиля"}
		if planOK {
			im.PullFrom = ih.PullFrom(ref)
		}
		out = append(out, localImageView{LocalImage: im, UsedBy: users[ref]})
	}
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

// handleImageFile — GET /api/local/ui/images/file?ref=&stage=&path=: файл
// контекста сборки ступени (текст) или каталог (список файлов).
func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request, id identity) {
	h, ok := s.imagesHooks()
	if !ok {
		writeErr(w, http.StatusNotImplemented, "файлы контекста недоступны в этой сборке")
		return
	}
	q := r.URL.Query()
	ref := strings.TrimSpace(q.Get("ref"))
	if err := s.validImageRef(ref); err != nil || ref == "" {
		writeErr(w, http.StatusBadRequest, "нужен образ: ?ref=<образ>")
		return
	}
	stage, err := strconv.Atoi(q.Get("stage"))
	if err != nil || stage < 1 {
		writeErr(w, http.StatusBadRequest, "нужна ступень: ?stage=<номер с 1, как в плане>")
		return
	}
	rel := strings.TrimSuffix(strings.TrimSpace(q.Get("path")), "/")
	if rel == "" || rel == "." || !fs.ValidPath(rel) || strings.Contains(rel, "\\") {
		writeErr(w, http.StatusBadRequest, "нужен путь внутри контекста сборки: ?path=<файл или каталог>, без «..»")
		return
	}
	f, err := h.ImageFile(ref, stage, rel)
	if err != nil {
		code := http.StatusUnprocessableEntity
		if errors.Is(err, fs.ErrNotExist) {
			code = http.StatusNotFound
		}
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type imageActionReq struct {
	Ref string `json:"ref"`
}

// handleImageAction — POST /api/local/ui/images/{ensure|pull|rebuild|remove}
// {ref}: скачать или собрать недостающий; скачать из реестра (и образ
// работающего места — новое применится при его перезапуске); пересобрать
// на этой машине (удалить тег и собрать: готовые нижние ступени и слои — из
// кэша Docker); удалить. Образ работающего места не пересобирается и не
// удаляется.
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
	if action != "ensure" && action != "pull" && action != "rebuild" && action != "remove" {
		writeErr(w, http.StatusNotFound, "действия: ensure, pull, rebuild, remove")
		return
	}
	running := runningNames(s.imageUsers(id.ID)[ref])
	if (action == "rebuild" || action == "remove") && len(running) > 0 {
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
		err := s.runImageAction(ctx, action, ref, id.ID, hooks, ih, t.logf)
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

// runningNames — «имена» работающих мест из списка.
func runningNames(uses []imageUse) []string {
	var out []string
	for _, u := range uses {
		if u.Running {
			out = append(out, "«"+u.Name+"»")
		}
	}
	return out
}

func (s *Server) runImageAction(ctx context.Context, action, ref string, owner int64, hooks LocalHooks, ih LocalImagesHooks, logf func(string)) error {
	present := false
	for _, im := range hooks.Images() {
		present = present || (im.Ref == ref && im.Present)
	}
	switch action {
	case "pull":
		updated, err := ih.PullImage(ctx, ref, logf)
		if err != nil {
			return err
		}
		// Контейнер работающего места остаётся на прежнем образе: новый тег
		// подхватит следующий старт — место помечается «нужен перезапуск».
		uses := s.imageUsers(owner)[ref]
		if names := runningNames(uses); updated && len(names) > 0 {
			logf("места " + strings.Join(names, ", ") + " работают на прежней версии образа — новая применится при перезапуске места")
			for _, u := range uses {
				if u.Running {
					s.markPlacePending(chatKey{uid: owner, slot: u.slot})
				}
			}
		}
		return nil
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
		// Собрать можно только образ семейства agctl-dsh этого профиля:
		// проверка до удаления, иначе прежний образ пропадёт зря.
		if _, err := ih.ImagePlan(ref); err != nil {
			return fmt.Errorf("на этой машине %s не собрать: %w", ref, err)
		}
		if present {
			if err := ih.RemoveImage(ctx, ref); err != nil {
				return fmt.Errorf("пересборка: удаление прежнего %s: %w", ref, err)
			}
			logf("прежний " + ref + " удалён — собираю на этой машине (готовые нижние ступени и слои — из кэша Docker)")
		}
		if err := ih.BuildImage(ctx, ref, logf); err != nil {
			return err
		}
		logf("✓ " + ref + " готов")
		return nil
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
