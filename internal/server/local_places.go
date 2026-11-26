package server

// local_places.go — лаунчер v2 (docs/architecture.md, раздел 6.9):
// несколько рабочих мест владельца в локальном режиме. Основное рабочее
// место (id "main") — прежнее: папка и образ из config.yaml, адрес /chat/.
// Дополнительные места хранятся в таблице places: своя папка (или несколько
// папок), свой образ, свой dsh-home и свой адрес /p/<id>/. Писать состояние
// может только сервер: изменение места передаёт ревизию, устаревшее
// отклоняется 409; обе открытые копии интерфейса узнают об изменениях из
// потока событий /api/local/ui/events.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/localmode"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/store"
)

// mainPlaceID — id основного рабочего места в API (в ключе рантайма — "").
const mainPlaceID = "main"

const maxPlaceMounts = 8

var (
	placeIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	mountNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// placeMount — папка хоста рабочего места. Name — подкаталог в /workspace
// (при одной папке не используется: она и есть /workspace).
type placeMount struct {
	Host string `json:"host"`
	Name string `json:"name"`
}

// localPlaceRec — рабочее место в памяти сервера: запись places + образ
// слота из settings (слот 0 — основное место из config.yaml).
type localPlaceRec struct {
	ID       string
	Slot     int
	Name     string
	Image    string
	Mounts   []placeMount
	Revision int64
}

// localPlaceBySlot — рабочее место слота key (слот > 0).
func (s *Server) localPlaceBySlot(key chatKey) (localPlaceRec, error) {
	if key.slot <= 0 {
		return localPlaceRec{}, fmt.Errorf("основное рабочее место не хранится в places")
	}
	p, err := s.db.PlaceBySlot(key.uid, key.slot)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return localPlaceRec{}, fmt.Errorf("у слота %d нет рабочего места", key.slot)
		}
		return localPlaceRec{}, err
	}
	return s.placeFromStore(p)
}

func (s *Server) placeFromStore(p store.Place) (localPlaceRec, error) {
	var mounts []placeMount
	if err := json.Unmarshal([]byte(p.Mounts), &mounts); err != nil {
		return localPlaceRec{}, fmt.Errorf("рабочее место %q: папки: %w", p.ID, err)
	}
	return localPlaceRec{ID: p.ID, Slot: p.Slot, Name: p.Name, Image: s.developerChatImage(p.UserID, p.Slot),
		Mounts: mounts, Revision: p.Revision}, nil
}

// placeSlot — слот рабочего места по id из адреса (main — слот 0).
func (s *Server) placeSlot(owner int64, pid string) (int, bool) {
	if pid == mainPlaceID {
		return 0, true
	}
	if !placeIDRe.MatchString(pid) {
		return 0, false
	}
	p, err := s.db.PlaceGet(owner, pid)
	if err != nil {
		return 0, false
	}
	return p.Slot, true
}

// freePlaceSlot — младший свободный слот 1..slotLimit-1 для нового места.
func (s *Server) freePlaceSlot(owner int64) (int, error) {
	rows, err := s.db.PlacesByUser(owner)
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, p := range rows {
		used[p.Slot] = true
	}
	for slot := 1; slot < s.slotLimit(); slot++ {
		if !used[slot] {
			return slot, nil
		}
	}
	return 0, fmt.Errorf("рабочих мест не больше %d (слоты лаунчера) — удалите ненужное", s.slotLimit())
}

// placeBinds — как папки места подключаются: одна — /workspace целиком,
// несколько — каждая в /workspace/<имя>.
func placeBinds(p localPlaceRec) (string, []agctlrt.Mount) {
	if len(p.Mounts) == 1 {
		return p.Mounts[0].Host, nil
	}
	out := make([]agctlrt.Mount, 0, len(p.Mounts))
	for _, m := range p.Mounts {
		out = append(out, agctlrt.Mount{Host: m.Host, Name: m.Name})
	}
	return "", out
}

// normalizeMounts проверяет папки (абсолютные, существуют, не $HOME целиком
// и не корень — как папка проектов agctl dev) и раздаёт имена подкаталогов.
func normalizeMounts(in []placeMount) ([]placeMount, error) {
	if len(in) == 0 {
		return nil, errors.New("выберите хотя бы одну папку")
	}
	if len(in) > maxPlaceMounts {
		return nil, fmt.Errorf("не больше %d папок на рабочее место", maxPlaceMounts)
	}
	out := make([]placeMount, 0, len(in))
	names, hosts := map[string]bool{}, map[string]bool{}
	for _, m := range in {
		abs, err := localmode.ValidateWorkDir(strings.TrimSpace(m.Host))
		if err != nil {
			return nil, err
		}
		if hosts[abs] {
			return nil, fmt.Errorf("папка %s выбрана дважды", abs)
		}
		hosts[abs] = true
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = slugName(filepath.Base(abs))
		}
		if !mountNameRe.MatchString(name) {
			return nil, fmt.Errorf("имя %q для /workspace: строчные латинские буквы, цифры, точка, дефис, подчёркивание", name)
		}
		base := name
		for i := 2; names[name]; i++ {
			name = base + "-" + strconv.Itoa(i)
		}
		names[name] = true
		out = append(out, placeMount{Host: abs, Name: name})
	}
	return out, nil
}

// slugName — имя каталога из имени папки: строчные латиница/цифры/дефис.
func slugName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		out = "folder"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// ── API ─────────────────────────────────────────────────────────────────────

func (s *Server) registerLocalPlaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/ui/places", s.requireLocalOwner(s.handlePlacesList))
	mux.HandleFunc("POST /api/local/ui/places", s.requireLocalOwner(s.handlePlaceCreate))
	mux.HandleFunc("PUT /api/local/ui/places/{id}", s.requireLocalOwner(s.handlePlaceUpdate))
	mux.HandleFunc("DELETE /api/local/ui/places/{id}", s.requireLocalOwner(s.handlePlaceDelete))
	mux.HandleFunc("POST /api/local/ui/places/{id}/{action}", s.requireLocalOwner(s.handlePlaceAction))
	mux.HandleFunc("GET /api/local/ui/places/{id}/logs", s.requireLocalOwner(s.handlePlaceLogs))
	mux.HandleFunc("GET /api/local/ui/fs", s.requireLocalOwner(s.handleLocalFS))
	mux.HandleFunc("POST /api/local/ui/fs/dialog", s.requireLocalOwner(s.handleLocalFSDialog))
	mux.HandleFunc("POST /api/local/ui/reveal", s.requireLocalOwner(s.handleLocalReveal))
	mux.HandleFunc("GET /api/local/ui/events", s.requireLocalOwner(s.handleLocalEvents))
	// Страницы лаунчера в локальном режиме (серверный модуль launcher здесь не
	// подключён): полный лаунчер и встроенный режим для табов правой панели dsh.
	mux.HandleFunc("GET /launcher", s.adminPageHandler(s.webFS))
	mux.HandleFunc("GET /launcher/embed/{section...}", s.adminPageHandler(s.webFS))
	// Чат дополнительного места: тот же шлюз, что /chat/, под своим префиксом.
	mux.HandleFunc("GET /p/{place}/", s.requireLocalOwner(s.placeChat))
	mux.HandleFunc("POST /p/{place}/", s.requireLocalOwner(s.placeChat))
	mux.HandleFunc("GET /p/{place}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/p/"+r.PathValue("place")+"/", http.StatusTemporaryRedirect)
	})
}

// placeView — рабочее место в ответах API.
type placeView struct {
	ID       string        `json:"id"`
	Slot     int           `json:"slot"`
	Name     string        `json:"name"`
	Main     bool          `json:"main"`
	Image    string        `json:"image"`
	Mounts   []placeMount  `json:"mounts"`
	Revision int64         `json:"revision"`
	URL      string        `json:"url"`
	DshHome  string        `json:"dshHome"`
	State    string        `json:"state"` // running | sleeping | starting | building | error
	Task     any           `json:"task"`
	Layout   []mountLayout `json:"layout"`
}

// mountLayout — где папка хоста видна в контейнере.
type mountLayout struct {
	Host      string `json:"host"`
	Container string `json:"container"`
}

// placeTask — задача подготовки слота (слот 0 — задача основного места).
func (s *Server) placeTask(slot int) *localTask {
	if slot == 0 {
		return s.local.task
	}
	s.local.placeMu.Lock()
	defer s.local.placeMu.Unlock()
	if s.local.placeTasks == nil {
		s.local.placeTasks = map[int]*localTask{}
	}
	t := s.local.placeTasks[slot]
	if t == nil {
		t = &localTask{state: "idle"}
		s.local.placeTasks[slot] = t
	}
	return t
}

func (s *Server) viewOf(owner int64, rec localPlaceRec, main bool) placeView {
	key := chatKey{uid: owner, slot: rec.Slot}
	v := placeView{ID: rec.ID, Slot: rec.Slot, Name: rec.Name, Main: main, Image: rec.Image, Mounts: rec.Mounts,
		Revision: rec.Revision, DshHome: s.dshHomeFor(key)}
	if main {
		v.URL = "/chat/"
	} else {
		v.URL = "/p/" + rec.ID + "/"
	}
	if v.Image == "" {
		v.Image = s.localHooks().CurrentImage()
	}
	if len(rec.Mounts) == 1 {
		v.Layout = []mountLayout{{Host: rec.Mounts[0].Host, Container: "/workspace"}}
	} else {
		for _, m := range rec.Mounts {
			v.Layout = append(v.Layout, mountLayout{Host: m.Host, Container: "/workspace/" + m.Name})
		}
	}
	t := s.placeTask(rec.Slot)
	snap := t.snapshot()
	v.Task = snap
	s.chatMu.Lock()
	running := s.chats[key] != nil
	s.chatMu.Unlock()
	switch st, _ := snap["state"].(string); {
	case st == "building" || st == "starting" || st == "error":
		v.State = st
	case running:
		v.State = "running"
	default:
		v.State = "sleeping"
	}
	return v
}

func (s *Server) mainPlace() localPlaceRec {
	wd := s.localWorkDir()
	return localPlaceRec{ID: mainPlaceID, Name: "Основное", Image: s.localHooks().CurrentImage(),
		Mounts: []placeMount{{Host: wd, Name: slugName(filepath.Base(wd))}}, Revision: s.local.mainRevision()}
}

func (s *Server) placeViews(owner int64) ([]placeView, error) {
	out := []placeView{s.viewOf(owner, s.mainPlace(), true)}
	rows, err := s.db.PlacesByUser(owner)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		rec, err := s.placeFromStore(p)
		if err != nil {
			return nil, err
		}
		out = append(out, s.viewOf(owner, rec, false))
	}
	return out, nil
}

func (s *Server) handlePlacesList(w http.ResponseWriter, r *http.Request, id identity) {
	views, err := s.placeViews(id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": views})
}

type placeReq struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Image    string       `json:"image"`
	Mounts   []placeMount `json:"mounts"`
	Revision int64        `json:"revision"`
}

// validImageRef — образ из каталога профиля или любой явный тег без пробелов.
func (s *Server) validImageRef(ref string) error {
	if ref == "" {
		return nil
	}
	if strings.ContainsAny(ref, " \t\n") || len(ref) > 256 {
		return fmt.Errorf("некорректный образ %q", ref)
	}
	return nil
}

func (s *Server) handlePlaceCreate(w http.ResponseWriter, r *http.Request, id identity) {
	var req placeReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	pid := strings.TrimSpace(req.ID)
	if pid == "" {
		pid = slugName(req.Name)
		if len(pid) > 32 {
			pid = strings.Trim(pid[:32], "-._")
		}
		pid = strings.NewReplacer(".", "-", "_", "-").Replace(pid)
	}
	if !placeIDRe.MatchString(pid) || pid == mainPlaceID {
		writeErr(w, http.StatusBadRequest, "id рабочего места: строчные латинские буквы, цифры и дефис, до 32 символов, не main")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeErr(w, http.StatusBadRequest, "имя рабочего места: от 1 до 64 символов")
		return
	}
	// Дефолт по вопросу владельца: место создаётся сразу с валидной формой —
	// если папки не выбраны, заведём <папка проектов>/<id>; выбор остаётся
	// правкой дефолта, а не обязательным шагом.
	mountsIn := req.Mounts
	if len(mountsIn) == 0 {
		host := filepath.Join(s.localWorkDir(), pid)
		if err := os.MkdirAll(host, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "не удалось создать папку по умолчанию: "+err.Error())
			return
		}
		mountsIn = []placeMount{{Host: host}}
	}
	mounts, err := normalizeMounts(mountsIn)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	image := strings.TrimSpace(req.Image)
	if err := s.validImageRef(image); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.db.PlaceGet(id.ID, pid); err == nil {
		writeErr(w, http.StatusConflict, "рабочее место с таким id уже есть")
		return
	}
	slot, err := s.freePlaceSlot(id.ID)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	// Образ слота — там же, где у серверного лаунчера (settings).
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, slot), image); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw, _ := json.Marshal(mounts)
	p, err := s.db.PlaceCreate(store.Place{UserID: id.ID, ID: pid, Slot: slot, Name: name, Mounts: string(raw)})
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "рабочее место с таким id или слотом уже есть")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, _ := s.placeFromStore(p)
	_ = s.db.Audit(id.Login, "local.place.create", pid, fmt.Sprintf("слот %d %s", slot, raw))
	s.local.events.publish("places")
	writeJSON(w, http.StatusCreated, s.viewOf(id.ID, rec, false))
}

// expectedRevision — ревизия из If-Match (приоритет) или из тела.
func expectedRevision(r *http.Request, body int64) int64 {
	if h := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"W/`); h != "" {
		if n, err := strconv.ParseInt(h, 10, 64); err == nil {
			return n
		}
	}
	return body
}

func (s *Server) handlePlaceUpdate(w http.ResponseWriter, r *http.Request, id identity) {
	var req placeReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	pid := r.PathValue("id")
	expect := expectedRevision(r, req.Revision)
	mounts, err := normalizeMounts(req.Mounts)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	image := strings.TrimSpace(req.Image)
	if err := s.validImageRef(image); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if pid == mainPlaceID {
		s.updateMainPlace(w, id, expect, image, mounts)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeErr(w, http.StatusBadRequest, "имя рабочего места: от 1 до 64 символов")
		return
	}
	raw, _ := json.Marshal(mounts)
	p, err := s.db.PlaceUpdate(store.Place{UserID: id.ID, ID: pid, Name: name, Mounts: string(raw)}, expect)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	case errors.Is(err, store.ErrStaleRevision):
		writeErr(w, http.StatusConflict, "рабочее место уже изменили в другом окне — обновите страницу")
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, p.Slot), image); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, _ := s.placeFromStore(p)
	s.markPlacePending(chatKey{uid: id.ID, slot: p.Slot})
	_ = s.db.Audit(id.Login, "local.place.update", pid, string(raw))
	s.local.events.publish("places")
	writeJSON(w, http.StatusOK, s.viewOf(id.ID, rec, false))
}

// updateMainPlace — основное место: папка и образ живут в config.yaml, у него
// одна папка; ревизия — счётчик изменений в памяти процесса.
func (s *Server) updateMainPlace(w http.ResponseWriter, id identity, expect int64, image string, mounts []placeMount) {
	if expect != s.local.mainRevision() {
		writeErr(w, http.StatusConflict, "рабочее место уже изменили в другом окне — обновите страницу")
		return
	}
	if len(mounts) != 1 {
		writeErr(w, http.StatusBadRequest, "у основного рабочего места одна папка; для нескольких папок создайте новое рабочее место")
		return
	}
	h := s.localHooks()
	abs, err := h.SetWorkDir(mounts[0].Host)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.setLocalWorkDir(abs)
	if image != "" && image != h.CurrentImage() {
		if err := h.SetImage(image); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.db.SettingSet(chatImageSettingKey(id.ID, 0), image); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.local.bumpMain()
	s.markPlacePending(userKey(id.ID))
	_ = s.db.Audit(id.Login, "local.place.update", mainPlaceID, abs)
	s.local.events.publish("places")
	writeJSON(w, http.StatusOK, s.viewOf(id.ID, s.mainPlace(), true))
}

// markPlacePending — работающее место после изменения требует перезапуска.
func (s *Server) markPlacePending(key chatKey) {
	s.chatMu.Lock()
	running := s.chats[key] != nil
	s.chatMu.Unlock()
	if running {
		s.placeTask(key.slot).markPending()
	}
}

func (s *Server) handlePlaceDelete(w http.ResponseWriter, r *http.Request, id identity) {
	pid := r.PathValue("id")
	if pid == mainPlaceID {
		writeErr(w, http.StatusBadRequest, "основное рабочее место удалить нельзя")
		return
	}
	p, err := s.db.PlaceGet(id.ID, pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	}
	key := chatKey{uid: id.ID, slot: p.Slot}
	if err := s.stopPlace(key); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.PlaceDelete(id.ID, pid); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.SettingSet(chatImageSettingKey(id.ID, p.Slot), "")
	// Слот освобождается для следующего места — его данные (dsh-home:
	// сессии, скиллы, настройки) уходят в архив рядом, а не наследуются.
	archived := s.archiveSlotStorage(key)
	s.local.placeMu.Lock()
	delete(s.local.placeTasks, p.Slot)
	s.local.placeMu.Unlock()
	_ = s.db.Audit(id.Login, "local.place.delete", pid, archived)
	s.local.events.publish("places")
	writeJSON(w, http.StatusOK, map[string]any{"id": pid, "deleted": true, "archived": archived})
}

// archiveSlotStorage переименовывает каталог данных слота
// ($AGCTL_HOME/chats/u<ID>s<N>) в u<ID>s<N>.deleted-<время>; папки хоста не
// трогаются. Возвращает путь архива ("" — данных не было).
func (s *Server) archiveSlotStorage(key chatKey) string {
	dir := filepath.Join(s.cfg.Home, "chats", chatStorageID(key.uid, key.slot))
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	dst := dir + ".deleted-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(dir, dst); err != nil {
		log.Printf("[local] архив данных слота %d: %v", key.slot, err)
		return ""
	}
	return dst
}

// stopPlace — остановить контейнер места (данные остаются на маунтах).
func (s *Server) stopPlace(key chatKey) error {
	s.chatMu.Lock()
	rt := s.chats[key]
	s.chatMu.Unlock()
	if rt == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s.forgetToken(chatTokenCacheKey(rt.handle))
	err := s.chatProvider.StopChat(ctx, rt.handle)
	s.evictChatByUser(key.uid, key.slot)
	return err
}

func (s *Server) handlePlaceAction(w http.ResponseWriter, r *http.Request, id identity) {
	pid, action := r.PathValue("id"), r.PathValue("action")
	slot, ok := s.placeSlot(id.ID, pid)
	if !ok {
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	}
	switch action {
	case "start", "restart":
		if !s.startPlaceRuntime(id.ID, slot, action == "restart") {
			writeErr(w, http.StatusConflict, "подготовка уже идёт")
			return
		}
		s.local.events.publish("places")
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "starting"})
	case "stop":
		if err := s.stopPlace(chatKey{uid: id.ID, slot: slot}); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.local.events.publish("places")
		writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
	default:
		writeErr(w, http.StatusNotFound, "неизвестное действие")
	}
}

// startPlaceRuntime — в фоне: (остановить) → образ есть? иначе скачать или
// собрать → старт. Основное место — прежний startLocalRuntime.
func (s *Server) startPlaceRuntime(owner int64, slot int, restart bool) bool {
	if slot == 0 {
		return s.startLocalRuntime(restart)
	}
	key := chatKey{uid: owner, slot: slot}
	rec, err := s.localPlaceBySlot(key)
	if err != nil {
		return false
	}
	pid := rec.ID
	ref := rec.Image
	if ref == "" {
		ref = s.localHooks().CurrentImage()
	}
	t := s.placeTask(slot)
	t.mu.Lock()
	if t.state == "building" || t.state == "starting" {
		t.mu.Unlock()
		return false
	}
	t.state, t.image, t.err, t.log = "starting", ref, "", nil
	t.mu.Unlock()
	go func() {
		defer s.local.events.publish("places")
		fail := func(err error) {
			t.mu.Lock()
			t.state, t.err = "error", err.Error()
			t.mu.Unlock()
		}
		if restart {
			t.logf("останавливаю рабочее место " + pid)
			if err := s.stopPlace(key); err != nil {
				fail(err)
				return
			}
		}
		present := false
		for _, im := range s.localHooks().Images() {
			present = present || (im.Ref == ref && im.Present)
		}
		if !present {
			t.mu.Lock()
			t.state = "building"
			t.mu.Unlock()
			s.local.events.publish("places")
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
			err := s.localHooks().EnsureImage(ctx, ref, t.logf)
			cancel()
			if err != nil {
				fail(err)
				return
			}
			t.mu.Lock()
			t.state = "starting"
			t.mu.Unlock()
		}
		t.logf("запускаю рабочее место " + pid + " на " + ref)
		// Пресеты места кладутся в dsh-home до entrypoint (local_presets.go).
		if err := s.ensureChatWithPresets(owner, slot, t.logf); err != nil {
			fail(err)
			return
		}
		t.logf("рабочее место запущено")
		t.mu.Lock()
		t.state, t.pending = "idle", false
		t.mu.Unlock()
	}()
	return true
}

func (s *Server) handlePlaceLogs(w http.ResponseWriter, r *http.Request, id identity) {
	slot, ok := s.placeSlot(id.ID, r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	}
	s.chatMu.Lock()
	rt := s.chats[chatKey{uid: id.ID, slot: slot}]
	s.chatMu.Unlock()
	if rt == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": "", "running": false})
		return
	}
	tail := r.URL.Query().Get("tail")
	if n, err := strconv.Atoi(tail); err != nil || n <= 0 || n > 5000 {
		tail = "300"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	logs, err := s.chatProvider.LogsChat(ctx, rt.handle, tail)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": redactLogs(logs), "running": true})
}

// redactLogs — токены загрузки dsh в логах контейнера не показываем.
var bootTokenRe = regexp.MustCompile(`([?&]token=)[^\s&"']+`)

func redactLogs(s string) string { return bootTokenRe.ReplaceAllString(s, "${1}…") }

// placeChat — чат дополнительного рабочего места под /p/<id>/.
func (s *Server) placeChat(w http.ResponseWriter, r *http.Request, id identity) {
	pid := r.PathValue("place")
	if pid == mainPlaceID {
		http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
		return
	}
	slot, ok := s.placeSlot(id.ID, pid)
	if !ok {
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	}
	s.chatProxyAt(w, r, id, slot, "/p/"+pid)
}

// ── папки хоста ─────────────────────────────────────────────────────────────

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git"`
}

// homeRoot — домашний каталог владельца (канонический путь).
func homeRoot() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(h); err == nil {
		h = r
	}
	return filepath.Clean(h), nil
}

// handleLocalFS — GET /api/local/ui/fs?path=&hidden=1: подкаталоги внутри
// домашнего каталога (дерево выбора папок). Выше $HOME не поднимается.
func (s *Server) handleLocalFS(w http.ResponseWriter, r *http.Request, id identity) {
	home, err := homeRoot()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = home
	}
	dir, err = filepath.Abs(dir)
	if err == nil {
		if rr, e := filepath.EvalSymlinks(dir); e == nil {
			dir = rr
		}
	}
	if err != nil || !pathUnder(home, dir) && filepath.Clean(dir) != home {
		writeErr(w, http.StatusForbidden, "можно выбирать только папки внутри домашнего каталога")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	showHidden := r.URL.Query().Get("hidden") == "1"
	out := []fsEntry{}
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		_, gitErr := os.Stat(filepath.Join(full, ".git"))
		out = append(out, fsEntry{Name: name, Path: full, Git: gitErr == nil})
		if len(out) >= 1000 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	parent := ""
	if dir != home {
		parent = filepath.Dir(dir)
	}
	_, selErr := localmode.ValidateWorkDir(dir)
	writeJSON(w, http.StatusOK, map[string]any{
		"path": dir, "parent": parent, "home": home, "entries": out,
		"selectable": selErr == nil, "reason": errString(selErr),
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ── системный диалог выбора папки ───────────────────────────────────────────

// dialogHook — подмена диалога в тестах.
var dialogHook func() (path string, cancelled bool, err error)

// folderDialogCommand — нативный диалог выбора папки платформы (окно Finder
// на macOS, zenity/kdialog на Linux, FolderBrowserDialog на Windows).
// Пустой stdout — пользователь отменил.
func folderDialogCommand() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Папка для рабочего места")`), nil
	case "windows":
		ps := `$d = New-Object System.Windows.Forms.FolderBrowserDialog; ` +
			`if ($d.ShowDialog() -eq 'OK') { Write-Output $d.SelectedPath } else { exit 1 }`
		return exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps), nil
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			return exec.Command("zenity", "--file-selection", "--directory"), nil
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command("kdialog", "--getexistingdirectory", os.Getenv("HOME")), nil
		}
		return nil, errors.New("нет системного диалога папки (zenity или kdialog) — выберите папку деревом")
	}
}

// handleLocalFSDialog — POST /api/local/ui/fs/dialog: показать системный
// диалог выбора папки и вернуть выбранный путь (без проверки selectable —
// её делает сохранение места). Отмена — {cancelled: true}. Диалог ждёт
// человека, поэтому не дольше 5 минут.
func (s *Server) handleLocalFSDialog(w http.ResponseWriter, r *http.Request, _ identity) {
	if dialogHook != nil {
		path, cancelled, err := dialogHook()
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeDialogResult(w, path, cancelled)
		return
	}
	cmd, err := folderDialogCommand()
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	out, err := cmd.Output()
	if ctx.Err() != nil {
		writeErr(w, http.StatusRequestTimeout, "диалог закрыт по таймауту — попробуйте ещё раз")
		return
	}
	if err != nil {
		// Отмена пользователя: osascript 128, zenity/kdialog/PowerShell 1.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			writeDialogResult(w, "", true)
			return
		}
		writeErr(w, http.StatusInternalServerError, "диалог не открылся: "+err.Error())
		return
	}
	writeDialogResult(w, strings.TrimSpace(string(out)), false)
}

func writeDialogResult(w http.ResponseWriter, path string, cancelled bool) {
	if cancelled || strings.TrimSpace(path) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
		return
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		if rr, e := filepath.EvalSymlinks(abs); e == nil {
			abs = rr
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": false, "path": abs})
}

type revealReq struct {
	Path string `json:"path"`
	App  string `json:"app"` // finder | vscode
}

// revealRoots — где разрешено открывать файлы: папки рабочих мест и их
// dsh-home (скиллы, настройки).
func (s *Server) revealRoots(owner int64) []string {
	var roots []string
	views, err := s.placeViews(owner)
	if err != nil {
		return nil
	}
	for _, v := range views {
		for _, m := range v.Mounts {
			roots = append(roots, m.Host)
		}
		roots = append(roots, v.DshHome)
	}
	return roots
}

// handleLocalReveal — POST /api/local/ui/reveal: показать путь в Finder или
// открыть в VS Code на машине разработчика. Только локальный режим (сервер
// слушает loopback) и только внутри папок и dsh-home рабочих мест.
func (s *Server) handleLocalReveal(w http.ResponseWriter, r *http.Request, id identity) {
	var req revealReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	p, err := filepath.Abs(strings.TrimSpace(req.Path))
	if err == nil {
		if rr, e := filepath.EvalSymlinks(p); e == nil {
			p = rr
		}
	}
	allowed := false
	for _, root := range s.revealRoots(id.ID) {
		if rr, e := filepath.EvalSymlinks(root); e == nil {
			root = rr
		}
		if filepath.Clean(p) == filepath.Clean(root) || pathUnder(root, p) {
			allowed = true
			break
		}
	}
	if err != nil || !allowed {
		writeErr(w, http.StatusForbidden, "открывать можно только файлы в папках рабочих мест и их dsh-home")
		return
	}
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "путь не найден")
		return
	}
	cmd, err := revealCommand(req.App, p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось открыть: "+err.Error())
		return
	}
	go func() { _ = cmd.Wait() }()
	writeJSON(w, http.StatusOK, map[string]any{"opened": p, "app": req.App})
}

// revealHook — подмена запуска в тестах.
var revealHook func(app, path string) (*exec.Cmd, error)

func revealCommand(app, p string) (*exec.Cmd, error) {
	if revealHook != nil {
		return revealHook(app, p)
	}
	switch app {
	case "finder", "":
		switch runtime.GOOS {
		case "darwin":
			return exec.Command("open", "-R", p), nil
		case "windows":
			return exec.Command("explorer", "/select,", p), nil
		default:
			dir := p
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				dir = filepath.Dir(p)
			}
			return exec.Command("xdg-open", dir), nil
		}
	case "vscode":
		if code, err := exec.LookPath("code"); err == nil {
			return exec.Command(code, p), nil
		}
		if runtime.GOOS == "darwin" {
			return exec.Command("open", "-a", "Visual Studio Code", p), nil
		}
		return nil, errors.New("не найдена команда code (VS Code → Shell Command: Install 'code' command in PATH)")
	}
	return nil, fmt.Errorf("неизвестное приложение %q", app)
}

// ── события ─────────────────────────────────────────────────────────────────

// localEvents — поток изменений для открытых копий лаунчера: тема и номер.
type localEvents struct {
	mu   sync.Mutex
	seq  int64
	subs map[chan string]struct{}
}

func (e *localEvents) publish(topic string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.seq++
	msg := fmt.Sprintf(`{"topic":%q,"seq":%d}`, topic, e.seq)
	for ch := range e.subs {
		select {
		case ch <- msg:
		default: // медленный подписчик пропустит событие и перечитает при следующем
		}
	}
	e.mu.Unlock()
}

func (e *localEvents) subscribe() (chan string, func()) {
	ch := make(chan string, 16)
	e.mu.Lock()
	if e.subs == nil {
		e.subs = map[chan string]struct{}{}
	}
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, ch)
		e.mu.Unlock()
	}
}

// handleLocalEvents — GET /api/local/ui/events: Server-Sent Events.
func (s *Server) handleLocalEvents(w http.ResponseWriter, r *http.Request, id identity) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": agctl\n\n")
	if err := rc.Flush(); err != nil {
		return
	}
	ch, cancel := s.local.events.subscribe()
	defer cancel()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", msg)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
