package server

// local_dshhome.go — явная папка dsh-home рабочего места (локальный режим):
// вместо <дом>/chats/<слот>/dsh-home место может жить в папке владельца,
// например ~/src/ag_dsh. Такая папка становится git-репозиторием
// конфигурации агента (.gitignore без секретов и сессий; первый коммит —
// после первого старта, когда dsh-home засеян), её можно залить на свой Git
// и подключить к другому месту — на этой машине или на другой. Путь хранится
// в settings (как образ слота), таблица places не меняется.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/store"
)

const dshHomeSettingPrefix = "local.dsh_home."

func dshHomeSettingKey(userID int64, slot int) string {
	if slot <= 0 {
		return dshHomeSettingPrefix + strconv.FormatInt(userID, 10)
	}
	return dshHomeSettingPrefix + strconv.FormatInt(userID, 10) + "-s" + strconv.Itoa(slot)
}

// LocalMainDshHomeHooks — своя папка dsh-home основного места живёт в
// config.yaml рядом с его папкой и образом: её видят и CLI-бэкапы
// (agctl dev update / restore), и agctl dev paths.
type LocalMainDshHomeHooks interface {
	MainDshHome() string
	SetMainDshHome(dir string) error
}

// customDshHome — явная папка dsh-home места ("" — по умолчанию, в доме
// agctl). Только локальный режим и только владелец.
func (s *Server) customDshHome(key chatKey) string {
	if !s.isLocal() || s.local == nil || key.uid != s.local.ownerID || s.db == nil {
		return ""
	}
	settingKey := dshHomeSettingKey(key.uid, key.slot)
	v, err := s.db.SettingGet(settingKey)
	if err != nil {
		v = ""
	}
	v = strings.TrimSpace(v)
	if h, ok := s.localHooks().(LocalMainDshHomeHooks); ok && key.slot == 0 {
		if cur := strings.TrimSpace(h.MainDshHome()); cur != "" || v == "" {
			return cur
		}
		// Ранняя запись основного места в settings — переносим в config.yaml.
		if err := h.SetMainDshHome(v); err != nil {
			log.Printf("[local] перенос dsh-home основного места в config.yaml: %v", err)
			return v
		}
		_ = s.db.SettingSet(settingKey, "")
	}
	return v
}

// defaultDshHome — dsh-home места в доме agctl (без явной папки).
func (s *Server) defaultDshHome(key chatKey) string {
	return filepath.Join(s.cfg.Home, "chats", chatStorageID(key.uid, key.slot), "dsh-home")
}

// validateDshHomeDir — явная папка dsh-home: абсолютный путь, не корень и
// не домашний каталог целиком, не внутри данных agctl (там dsh-home мест по
// умолчанию). Папки может не быть — создастся.
func (s *Server) validateDshHomeDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if strings.HasPrefix(dir, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(h, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("папка dsh-home: нужен абсолютный путь, а не %q", dir)
	}
	abs := canonicalPath(filepath.Clean(dir))
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return "", errors.New("папка dsh-home: корень файловой системы не подходит")
	}
	if h, err := homeRoot(); err == nil && abs == h {
		return "", errors.New("папка dsh-home: домашний каталог целиком не подходит — возьмите подпапку, например ~/src/ag_dsh")
	}
	data := canonicalPath(filepath.Clean(s.cfg.Home))
	if abs == data || pathUnder(data, abs) {
		return "", errors.New("папка dsh-home: внутри данных agctl уже лежат dsh-home мест по умолчанию — выберите папку вне " + data)
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		return "", fmt.Errorf("папка dsh-home %s — это файл", abs)
	}
	return abs, nil
}

// canonicalPath — путь с раскрытыми симлинками, даже если его хвоста ещё
// нет (/var → /private/var на macOS): раскрывается ближайший существующий
// предок, остаток пристёгивается как есть.
func canonicalPath(p string) string {
	rest := ""
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// setCustomDshHome — запомнить явную папку dsh-home места ("" — вернуть
// место в дом agctl) и подготовить её как git-репозиторий конфигурации.
// Возвращает заметку для журнала (без git — папка работает и так).
func (s *Server) setCustomDshHome(owner int64, slot int, dir string) (string, error) {
	if h, ok := s.localHooks().(LocalMainDshHomeHooks); ok && slot == 0 {
		if err := h.SetMainDshHome(dir); err != nil {
			return "", err
		}
	} else if err := s.db.SettingSet(dshHomeSettingKey(owner, slot), dir); err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("папка dsh-home: %w", err)
	}
	created, err := provision.InitDshHomeRepo(dir)
	switch {
	case errors.Is(err, provision.ErrNoGit):
		return "git не найден — dsh-home работает без версионирования", nil
	case err != nil:
		return "", fmt.Errorf("git в папке dsh-home: %w", err)
	case created:
		return "dsh-home — новый git-репозиторий (.gitignore без секретов и сессий); первый коммит — после первого старта", nil
	}
	return "", nil
}

// commitDshHomeIfNew — первый коммит конфигурации в явной папке dsh-home,
// когда рантайм её засеял (после старта места). Дальше коммитит владелец
// или агент — как в любом репозитории.
func (s *Server) commitDshHomeIfNew(key chatKey, logf func(string)) {
	dir := s.customDshHome(key)
	if dir == "" {
		return
	}
	st := provision.DshHomeRepoState(dir)
	if !st.Repo || st.Commits {
		return
	}
	ok, err := provision.CommitDshHome(dir, "agctl: начальная конфигурация dsh-home")
	if err != nil {
		log.Printf("[local] первый коммит dsh-home %s: %v", dir, err)
		return
	}
	if ok {
		logf("dsh-home: первый коммит конфигурации в " + dir + " — залейте репозиторий на свой Git, чтобы подключать его к другим местам")
	}
}

// ── клон рабочего места ─────────────────────────────────────────────────────

type placeCloneReq struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Start bool   `json:"start"`
}

// handlePlaceClone — POST /api/local/ui/places/{id}/clone: новое место с теми
// же папками и образом и копией dsh-home исходного (сессии, скиллы,
// настройки, пресеты) — пробовать новую версию образа или плагины без риска
// для основного места; заодно ручной бэкап. Исходное место на время копии
// останавливается (иначе SQLite dsh может скопироваться несогласованным) и
// затем запускается снова. Копия dsh-home ложится в дом agctl (на macOS —
// клон APFS без копирования данных).
func (s *Server) handlePlaceClone(w http.ResponseWriter, r *http.Request, id identity, srcID string) {
	var req placeCloneReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	srcSlot, ok := s.placeSlot(id.ID, srcID)
	if !ok {
		writeErr(w, http.StatusNotFound, "рабочее место не найдено")
		return
	}
	var src localPlaceRec
	if srcSlot == 0 {
		src = s.mainPlace()
	} else {
		rec, err := s.localPlaceBySlot(chatKey{uid: id.ID, slot: srcSlot})
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		src = rec
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + " (клон)"
	}
	if len([]rune(name)) > 64 {
		writeErr(w, http.StatusBadRequest, "имя рабочего места: от 1 до 64 символов")
		return
	}
	pid := strings.TrimSpace(req.ID)
	if pid == "" {
		base := slugName(src.ID + "-clone")
		base = strings.NewReplacer(".", "-", "_", "-").Replace(base)
		if len(base) > 28 {
			base = strings.Trim(base[:28], "-")
		}
		pid = base
		for i := 2; ; i++ {
			if _, err := s.db.PlaceGet(id.ID, pid); err != nil {
				break
			}
			pid = fmt.Sprintf("%s-%d", base, i)
		}
	}
	if !placeIDRe.MatchString(pid) || pid == mainPlaceID {
		writeErr(w, http.StatusBadRequest, "id рабочего места: строчные латинские буквы, цифры и дефис, до 32 символов, не main")
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
	srcKey, dstKey := chatKey{uid: id.ID, slot: srcSlot}, chatKey{uid: id.ID, slot: slot}
	if s.placeTask(srcSlot).busy() {
		writeErr(w, http.StatusConflict, "исходное место сейчас запускается — дождитесь и повторите")
		return
	}
	// Копия — при остановленном источнике и под его замком первого старта:
	// открытая вкладка чата не поднимет место посреди копирования (её запрос
	// подождёт), иначе SQLite и сессии dsh скопировались бы рваными.
	srcLock := s.chatStartLock(srcKey)
	srcLock.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			srcLock.Unlock()
		}
	}
	defer unlock()
	s.chatMu.Lock()
	wasRunning := s.chats[srcKey] != nil
	s.chatMu.Unlock()
	if wasRunning {
		if err := s.stopPlace(srcKey); err != nil {
			writeErr(w, http.StatusInternalServerError, "остановка исходного места: "+err.Error())
			return
		}
	}
	restartSrc := func() {
		unlock()
		if wasRunning {
			s.startPlaceRuntime(id.ID, srcSlot, false)
		}
	}
	// Слот мог остаться с данными удалённого места — в архив, не в клон.
	if archived := s.archiveSlotStorage(dstKey); archived != "" {
		log.Printf("[local] клон: прежние данные слота %d → %s", slot, archived)
	}
	srcHome, dstHome := s.dshHomeFor(srcKey), s.defaultDshHome(dstKey)
	how := "пусто"
	if _, err := os.Stat(srcHome); err == nil {
		if how, err = provision.CloneDir(srcHome, dstHome); err != nil {
			restartSrc()
			writeErr(w, http.StatusInternalServerError, "копия dsh-home: "+err.Error())
			return
		}
	}
	mounts := src.Mounts
	raw, _ := json.Marshal(mounts)
	image := src.Image
	if srcSlot == 0 {
		image = s.developerChatImage(id.ID, 0)
	} else {
		image = s.developerChatImage(id.ID, srcSlot)
	}
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, slot), image); err != nil {
		restartSrc()
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.SettingSet(dshHomeSettingKey(id.ID, slot), "")
	p, err := s.db.PlaceCreate(store.Place{UserID: id.ID, ID: pid, Slot: slot, Name: name, Mounts: string(raw)})
	if err != nil {
		restartSrc()
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Пресеты исходного места — и на клоне (файлы уже в копии dsh-home).
	if presets, err := s.loadLocalPresets(); err == nil {
		changed := false
		for _, pr := range presets {
			for _, placeID := range pr.Places {
				if placeID == src.ID {
					pr.Places = append(pr.Places, pid)
					changed = true
					break
				}
			}
		}
		if changed {
			if err := s.saveLocalPresets(presets); err != nil {
				log.Printf("[local] клон: пресеты: %v", err)
			}
		}
	}
	restartSrc()
	t := s.placeTask(slot)
	t.logf(fmt.Sprintf("клон места «%s»: dsh-home скопирован (%s), папки и образ — те же", src.Name, how))
	if req.Start {
		s.startPlaceRuntime(id.ID, slot, false)
	}
	rec, _ := s.placeFromStore(p)
	_ = s.db.Audit(id.Login, "local.place.clone", pid, fmt.Sprintf("из %s, слот %d, dsh-home %s", src.ID, slot, how))
	s.local.events.publish("places")
	s.local.events.publish("presets")
	writeJSON(w, http.StatusCreated, s.viewOf(id.ID, rec, false))
}

// chatStartLock — замок первого старта слота (тот же, что у ensureChat).
func (s *Server) chatStartLock(key chatKey) *sync.Mutex {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chatLocks == nil {
		s.chatLocks = make(map[chatKey]*sync.Mutex)
	}
	lock := s.chatLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.chatLocks[key] = lock
	}
	return lock
}
