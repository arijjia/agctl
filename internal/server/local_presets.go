package server

// local_presets.go — пресеты возможностей лаунчера v2: именованные наборы
// MCP-серверов платформенного каталога и скиллов из каталога профиля,
// применяемые к выбранным рабочим местам. agctl материализует пресет в
// dsh-home мест: .agent-presets/agctl-<id>/agent.cordis.yml (MCP-ряды cordis)
// и skills/<имя>/… (скиллы контейнера); в dsh разработчик при создании сессии
// просто выбирает пресет. Пресет собирается в лаунчере галочками ДО запуска
// места. Хранение — один KV-ключ settings (JSON всех пресетов, без секретов);
// изменение передаёт ревизию, устаревшая отклоняется 409, открытые копии
// интерфейса обновляются из потока событий.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/store"
)

// presetIDRe — id пресета безопасен как сегмент каталога; префикс «agctl-»
// agctl дописывает сам (марка managed-пресета).
var presetIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// presetManagedID — id материализованного пресета в dsh-roster.
func presetManagedID(id string) string { return "agctl-" + id }

// localPresetsKey — ключ KV со всеми пресетами (JSON, без секретных значений:
// env серверов подставляется при материализации из доступов машины).
const localPresetsKey = "local-presets-v1"

// localPreset — пресет в хранении и API.
type localPreset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MCP         []string `json:"mcp"`    // id каталога PlatformMcpServers
	Skills      []string `json:"skills"` // id пакетов каталога профиля
	Places      []string `json:"places"` // id рабочих мест (main — основное)
	Revision    int64    `json:"revision"`
}

// loadLocalPresets — все пресеты из KV; ключа ещё нет — пустая карта.
func (s *Server) loadLocalPresets() (map[string]*localPreset, error) {
	out := map[string]*localPreset{}
	raw, err := s.db.SettingGet(localPresetsKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return out, nil
		}
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("пресеты: сохранённый JSON повреждён: %w", err)
	}
	return out, nil
}

func (s *Server) saveLocalPresets(m map[string]*localPreset) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.db.SettingSet(localPresetsKey, string(raw))
}

// sortedPresetIDs — детерминированный порядок списка.
func sortedPresetIDs(m map[string]*localPreset) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ── материализация ──────────────────────────────────────────────────────────

// presetSpecs — спецификации MCP выбранного состава (валидация уже сделана).
func (s *Server) presetSpecs(p *localPreset) []provision.McpServerSpec {
	specs := make([]provision.McpServerSpec, 0, len(p.MCP))
	for _, id := range p.MCP {
		if spec, ok := s.mcpSpec(id); ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

// applyPresetAt — материализовать пресет в dsh-home одного места. Ошибки
// возвращаются списком: часть файлов может встать, а скилл — нет.
func (s *Server) applyPresetAt(owner int64, p *localPreset, placeID string) error {
	dshHome, err := s.placeDshHome(owner, placeID)
	if err != nil {
		return err
	}
	// env серверов — доступы этой машины (хранилище ОС, мост); значения
	// секретов в KV и ответы API не попадают — только в файл пресета 0600.
	secretEnv := s.containerEnvMap(owner)
	desc := strings.TrimSpace(p.Description)
	if desc == "" {
		desc = "Набор из лаунчера agctl"
	}
	if err := provision.EnsureAgentPreset(dshHome, presetManagedID(p.ID), p.Name, desc,
		s.presetSpecs(p), secretEnv); err != nil {
		return err
	}
	// Скиллы пресета живут в его каталоге и видны только сессиям с этим
	// пресетом (guest подключает каталог в skill-filesystem пресета). Каталог
	// пересобирается целиком: снятый с пресета скилл исчезает.
	skillsRoot := provision.PresetSkillsRoot(dshHome, presetManagedID(p.ID))
	if err := os.RemoveAll(skillsRoot); err != nil {
		return err
	}
	for _, sid := range p.Skills {
		pkg, err := provision.FindCatalogPackage(s.brand.FS, sid)
		if err != nil {
			return err
		}
		if err := provision.InstallSkillPackageTo(skillsRoot, s.brand.FS, pkg); err != nil {
			return err
		}
	}
	return nil
}

// unapplyPresetAt — убрать файлы пресета из dsh-home места.
func (s *Server) unapplyPresetAt(owner int64, p *localPreset, placeID string) error {
	dshHome, err := s.placeDshHome(owner, placeID)
	if err != nil {
		return nil // места уже нет — снимать нечего (данные слота в архиве)
	}
	if err := provision.RemoveMcpPreset(dshHome, presetManagedID(p.ID)); err != nil {
		return err
	}
	for _, sid := range p.Skills {
		pkg, err := provision.FindCatalogPackage(s.brand.FS, sid)
		if err != nil {
			return err // каталог профиля мог измениться — сообщаем
		}
		// Прежние сборки ставили скиллы пресета в общий <dshHome>/skills —
		// подчищаем этот след; изменённый пользователем файл остаётся.
		if err := provision.RemoveSkillPackageFromDshHome(dshHome, s.brand.FS, pkg); err != nil {
			log.Printf("[presets] %s: общий skills/: %v", p.ID, err)
		}
	}
	return nil
}

// placeDshHome — dsh-home рабочего места по его id (через слот).
func (s *Server) placeDshHome(owner int64, placeID string) (string, error) {
	slot, ok := s.placeSlot(owner, placeID)
	if !ok {
		return "", fmt.Errorf("рабочее место %q не найдено", placeID)
	}
	return s.dshHomeFor(chatKey{uid: owner, slot: slot}), nil
}

// applyAllPresetsAt — при старте слота: накатить все пресеты, куда входит
// его рабочее место (ошибки в лог — старт не срываем, пресет можно
// пересохранить).
func (s *Server) applyAllPresetsAt(owner int64, slot int) int {
	placeID := mainPlaceID
	if slot != 0 {
		p, err := s.db.PlaceBySlot(owner, slot)
		if err != nil {
			return 0
		}
		placeID = p.ID
	}
	presets, err := s.loadLocalPresets()
	if err != nil {
		log.Printf("[presets] место %s: не прочитаны: %v", placeID, err)
		return 0
	}
	applied := 0
	for _, id := range sortedPresetIDs(presets) {
		p := presets[id]
		contains := false
		for _, pid := range p.Places {
			contains = contains || pid == placeID
		}
		if !contains {
			continue
		}
		if err := s.applyPresetAt(owner, p, placeID); err != nil {
			log.Printf("[presets] %s → %s: %v", id, placeID, err)
			continue
		}
		applied++
	}
	return applied
}

// ensureChatWithPresets — старт контейнера слота так, чтобы пресеты места
// лежали в dsh-home ДО entrypoint: там `agctl presets-sync` собирает из них
// патч для `dsh web --patch`, а работающий dsh каталог .agent-presets не
// перечитывает. Существующий dsh-home получает пресеты до старта; новый
// dsh-home сеет рантайм при первом старте — тогда пресеты кладутся после, и
// место один раз перезапускается (сессий у нового места ещё нет).
func (s *Server) ensureChatWithPresets(owner int64, slot int, logf func(string)) error {
	key := chatKey{uid: owner, slot: slot}
	seeded := dshHomeSeeded(s.dshHomeFor(key))
	if seeded {
		s.applyAllPresetsAt(owner, slot)
	}
	if _, err := s.ensureChat(owner, slot); err != nil {
		return err
	}
	if n := s.applyAllPresetsAt(owner, slot); n > 0 && !seeded {
		logf(fmt.Sprintf("пресетов места: %d — первый старт, перезапускаю, чтобы dsh их увидел", n))
		if err := s.stopPlace(key); err != nil {
			return err
		}
		if _, err := s.ensureChat(owner, slot); err != nil {
			return err
		}
	}
	return nil
}

// dshHomeSeeded — dsh-home уже засеян (тот же признак, что у рантайма).
func dshHomeSeeded(home string) bool {
	_, err := os.Stat(filepath.Join(home, "profiles", "web", "package.json"))
	return err == nil
}

// markPresetPlacesPending — работающие места, чей набор пресетов изменился,
// показывают «нужен перезапуск»: dsh видит пресеты только со старта.
func (s *Server) markPresetPlacesPending(owner int64, placeIDs ...[]string) {
	seen := map[string]bool{}
	for _, ids := range placeIDs {
		for _, pid := range ids {
			if seen[pid] {
				continue
			}
			seen[pid] = true
			if slot, ok := s.placeSlot(owner, pid); ok {
				s.markPlacePending(chatKey{uid: owner, slot: slot})
			}
		}
	}
	s.local.events.publish("places")
}

// ── API ─────────────────────────────────────────────────────────────────────

func (s *Server) registerLocalPresetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/ui/presets", s.requireLocalOwner(s.handlePresetsList))
	mux.HandleFunc("POST /api/local/ui/presets", s.requireLocalOwner(s.handlePresetCreate))
	mux.HandleFunc("PUT /api/local/ui/presets/{id}", s.requireLocalOwner(s.handlePresetUpdate))
	mux.HandleFunc("DELETE /api/local/ui/presets/{id}", s.requireLocalOwner(s.handlePresetDelete))
}

type presetReq struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MCP         []string `json:"mcp"`
	Skills      []string `json:"skills"`
	Places      []string `json:"places"`
	Revision    int64    `json:"revision"`
}

// validatePresetReq — нормализация и проверка состава. Возвращает нормализованный
// пресет без ревизии (id, уникальные отсортированные списки).
func (s *Server) validatePresetReq(req presetReq) (localPreset, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return localPreset{}, errors.New("имя пресета: от 1 до 64 символов")
	}
	pid := strings.TrimSpace(req.ID)
	if pid == "" {
		pid = slugName(translitRu(name))
		if len(pid) > 32 {
			pid = strings.Trim(pid[:32], "-._")
		}
		pid = strings.NewReplacer(".", "-", "_", "-").Replace(pid)
		for strings.Contains(pid, "--") {
			pid = strings.ReplaceAll(pid, "--", "-")
		}
	}
	if !presetIDRe.MatchString(pid) {
		return localPreset{}, errors.New("id пресета: строчные латинские буквы, цифры и дефис, до 32 символов")
	}
	out := localPreset{ID: pid, Name: name, Description: strings.TrimSpace(req.Description)}
	out.MCP = dedupeIDs(req.MCP)
	out.Skills = dedupeIDs(req.Skills)
	out.Places = dedupeIDs(req.Places)
	for _, sid := range out.MCP {
		if _, ok := s.mcpSpec(sid); !ok {
			return localPreset{}, fmt.Errorf("неизвестный MCP-сервер: %s", sid)
		}
	}
	for _, sid := range out.Skills {
		if _, err := provision.FindCatalogPackage(s.brand.FS, sid); err != nil {
			return localPreset{}, fmt.Errorf("нет пакета скиллов %q в каталоге профиля", sid)
		}
	}
	return out, nil
}

// dedupeIDs — убрать повторы, сохранив порядок.
func dedupeIDs(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// validatePresetPlaces — места из запроса существуют (main или из places).
func (s *Server) validatePresetPlaces(owner int64, places []string) error {
	for _, pid := range places {
		if _, ok := s.placeSlot(owner, pid); !ok {
			return fmt.Errorf("рабочего места %q нет", pid)
		}
	}
	return nil
}

// handlePresetsList — GET: пресеты + каталоги для конструктора (MCP и скиллы)
// + краткий список мест для галочек применения.
func (s *Server) handlePresetsList(w http.ResponseWriter, r *http.Request, id identity) {
	presets, err := s.loadLocalPresets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// missing — каких доступов не хватает серверам пресета: такие серверы в
	// харнессе отключены, лаунчер показывает причину.
	type presetView struct {
		*localPreset
		Missing map[string][]string `json:"missing,omitempty"`
	}
	env := s.containerEnvMap(id.ID)
	out := []presetView{}
	for _, pid := range sortedPresetIDs(presets) {
		v := presetView{localPreset: presets[pid]}
		for _, spec := range s.presetSpecs(presets[pid]) {
			if m := spec.MissingEnv(env); len(m) > 0 {
				if v.Missing == nil {
					v.Missing = map[string][]string{}
				}
				v.Missing[spec.ID] = m
			}
		}
		out = append(out, v)
	}
	mcp := []map[string]any{}
	for _, spec := range s.mcpCatalog() {
		mcp = append(mcp, map[string]any{
			"id": spec.ID, "title": spec.Title, "description": spec.Description, "secretEnv": spec.SecretEnv,
		})
	}
	skills := []map[string]any{}
	for _, pkg := range s.catalogPackages() {
		if len(provision.SkillPackageFiles(pkg)) == 0 {
			continue // пакет без скиллов (только MCP или файлы воркспейса)
		}
		skills = append(skills, map[string]any{"id": pkg.ID, "title": pkg.Title, "description": pkg.Description})
	}
	views, err := s.placeViews(id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	places := []map[string]any{}
	for _, v := range views {
		places = append(places, map[string]any{"id": v.ID, "name": v.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": out, "mcp": mcp, "skills": skills, "places": places})
}

// materializePresetDiff — применить пресет к новым местам и снять с убранных.
func (s *Server) materializePresetDiff(owner int64, old, cur *localPreset) error {
	places := map[string]bool{}
	for _, pid := range cur.Places {
		places[pid] = true
	}
	oldPlaces := map[string]bool{}
	if old != nil {
		for _, pid := range old.Places {
			oldPlaces[pid] = true
		}
	}
	var errs []string
	for _, pid := range cur.Places {
		if !oldPlaces[pid] {
			if err := s.applyPresetAt(owner, cur, pid); err != nil {
				errs = append(errs, pid+": "+err.Error())
			}
		}
	}
	if old != nil {
		// Состав изменился — на всех старых местах пресет пересобирается;
		// с убранных — снимается.
		changed := old.Name != cur.Name || old.Description != cur.Description ||
			!sameIDSet(old.MCP, cur.MCP) || !sameIDSet(old.Skills, cur.Skills)
		for _, pid := range old.Places {
			if !places[pid] {
				if err := s.unapplyPresetAt(owner, old, pid); err != nil {
					errs = append(errs, pid+": "+err.Error())
				}
			} else if changed {
				if err := s.applyPresetAt(owner, cur, pid); err != nil {
					errs = append(errs, pid+": "+err.Error())
				}
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func sameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

func (s *Server) handlePresetCreate(w http.ResponseWriter, r *http.Request, id identity) {
	var req presetReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	p, err := s.validatePresetReq(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validatePresetPlaces(id.ID, p.Places); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	presets, err := s.loadLocalPresets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, taken := presets[p.ID]; taken {
		writeErr(w, http.StatusConflict, "пресет с таким id уже есть")
		return
	}
	p.Revision = 1
	presets[p.ID] = &p
	if err := s.materializePresetDiff(id.ID, nil, &p); err != nil {
		writeErr(w, http.StatusInternalServerError, "пресет сохранён, но не везде применился: "+err.Error())
	}
	if err := s.saveLocalPresets(presets); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "local.preset.create", p.ID, fmt.Sprintf("mcp=%s skills=%s places=%s", joinIDs(p.MCP), joinIDs(p.Skills), joinIDs(p.Places)))
	s.markPresetPlacesPending(id.ID, p.Places)
	s.local.events.publish("presets")
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handlePresetUpdate(w http.ResponseWriter, r *http.Request, id identity) {
	var req presetReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	pid := r.PathValue("id")
	expect := expectedRevision(r, req.Revision)
	presets, err := s.loadLocalPresets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	old, ok := presets[pid]
	if !ok {
		writeErr(w, http.StatusNotFound, "пресет не найден")
		return
	}
	if expect != old.Revision {
		writeErr(w, http.StatusConflict, "пресет уже изменили в другом окне — обновите страницу")
		return
	}
	cur, err := s.validatePresetReq(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validatePresetPlaces(id.ID, cur.Places); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur.ID = pid
	cur.Revision = old.Revision + 1
	if err := s.materializePresetDiff(id.ID, old, &cur); err != nil {
		writeErr(w, http.StatusInternalServerError, "пресет сохранён, но не везде применился: "+err.Error())
	}
	presets[pid] = &cur
	if err := s.saveLocalPresets(presets); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "local.preset.update", pid, fmt.Sprintf("rev=%d mcp=%s skills=%s places=%s", cur.Revision, joinIDs(cur.MCP), joinIDs(cur.Skills), joinIDs(cur.Places)))
	s.markPresetPlacesPending(id.ID, old.Places, cur.Places)
	s.local.events.publish("presets")
	writeJSON(w, http.StatusOK, cur)
}

func (s *Server) handlePresetDelete(w http.ResponseWriter, r *http.Request, id identity) {
	pid := r.PathValue("id")
	presets, err := s.loadLocalPresets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	old, ok := presets[pid]
	if !ok {
		writeErr(w, http.StatusNotFound, "пресет не найден")
		return
	}
	var errs []string
	for _, placeID := range old.Places {
		if err := s.unapplyPresetAt(id.ID, old, placeID); err != nil {
			errs = append(errs, placeID+": "+err.Error())
		}
	}
	delete(presets, pid)
	if err := s.saveLocalPresets(presets); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "local.preset.delete", pid, "")
	s.markPresetPlacesPending(id.ID, old.Places)
	s.local.events.publish("presets")
	resp := map[string]any{"id": pid, "deleted": true}
	if len(errs) > 0 {
		resp["warning"] = strings.Join(errs, "; ")
	}
	writeJSON(w, http.StatusOK, resp)
}

// translitRu — латиница вместо кириллицы для id пресета: «Аналитика 1С» →
// «analitika 1s». Без неё кириллическое имя давало id «folder», и второй
// такой пресет упирался в «уже есть».
func translitRu(s string) string {
	table := map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
		'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
		'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts",
		'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if t, ok := table[r]; ok {
			b.WriteString(t)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
