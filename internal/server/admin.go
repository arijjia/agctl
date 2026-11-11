package server

// admin.go — админ-панель супер-админа (карточка admin-panel-super-admin).
// Все маршруты под requireAdmin. Админ — не пользователь платформы: личный
// dsh-контейнер ему не поднимается (см. rootEntry/chatProxy), зато он видит
// сводку, пользователей, контейнеры, журнал запросов шлюза и квоты.

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/store"
)

// containerLister / containerStatser — опциональные способности docker-рантайма
// (по образцу healthReporter): подделки в тестах их не реализуют, обработчики
// деградируют до пустого списка/503.
type containerLister interface {
	ChatContainers(ctx context.Context) ([]dockerrt.ChatContainerInfo, error)
}

type containerStatser interface {
	ContainerStatsSample(ctx context.Context, id string) (uint64, float64, error)
}

func (s *Server) listChatContainers() ([]dockerrt.ChatContainerInfo, bool) {
	if cl, ok := s.rt.(containerLister); ok {
		list, err := cl.ChatContainers(context.Background())
		if err == nil {
			return list, true
		}
	}
	return nil, false
}

func (s *Server) containerStats(id string) (uint64, float64, bool) {
	if cs, ok := s.rt.(containerStatser); ok {
		mem, cpu, err := cs.ContainerStatsSample(context.Background(), id)
		if err == nil {
			return mem, cpu, true
		}
	}
	return 0, 0, false
}

// pathInt64 — числовой {id} из пути; err при отсутствующем/нечисловом.
func pathInt64(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// handleAdminOverview — GET /api/admin/overview: сводка платформы.
func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request, _ identity) {
	users, err := s.db.UsersList()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	byRole := map[string]int{"user": 0, "developer": 0, "admin": 0}
	for _, u := range users {
		byRole[u.Role]++
	}
	sessionsCount, _ := s.db.SessionsCount()
	requestsToday, _ := s.db.ProxyRequestsCountToday()

	containers, _ := s.listChatContainers()
	running := 0
	memTotal := uint64(0)
	for _, c := range containers {
		if c.State == "running" {
			running++
			if mem, _, ok := s.containerStats(c.ID); ok {
				memTotal += mem
			}
		}
	}
	// Prewarm-пул (карточка chat-prewarm-pool): hit-rate тёплых сред
	// и среднее время холодного старта — метрики R1.3.
	coldN := s.coldStarts.Load()
	coldAvgMS := float64(0)
	if coldN > 0 {
		coldAvgMS = float64(s.coldTotalMS.Load()) / float64(coldN)
	}
	warmN := s.warmHits.Load()
	hitRate := float64(0)
	if warmN+coldN > 0 {
		hitRate = float64(warmN) / float64(warmN+coldN)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users": map[string]any{
			"total":  len(users),
			"byRole": byRole,
		},
		"sessions":       sessionsCount,
		"requestsToday":  requestsToday,
		"containers":     map[string]any{"known": len(containers), "running": running},
		"memoryBytes":    memTotal,
		"image":          s.cfg.Image,
		"chatTTLMinutes": int(s.cfg.ChatTTL / time.Minute),
		"chatPrewarm": map[string]any{
			"enabled":      s.prewarmEnabled(),
			"poolSize":     s.cfg.ChatPrewarmN,
			"warmNow":      s.warmCount(),
			"everyMinutes": int(s.cfg.ChatPrewarmEvery / time.Minute),
			"poolHitRate":  hitRate,
			"warmHits":     warmN,
			"coldStarts":   coldN,
			"coldAvgMs":    coldAvgMS,
		},
		"version":   s.cfg.Version,
		"startedAt": s.startedAt.UTC().Format(time.RFC3339),
	})
}

// handleAdminUsersDetailed — GET /api/admin/users/details: учётки с живым
// состоянием рантайма. ?withSizes=1 добавляет размер тома (обход файлов).
func (s *Server) handleAdminUsersDetailed(w http.ResponseWriter, r *http.Request, id identity) {
	_ = id
	users, err := s.db.UsersList()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	withSizes := r.URL.Query().Get("withSizes") == "1"
	containers, _ := s.listChatContainers()
	byUser := map[int64]dockerrt.ChatContainerInfo{}
	for _, c := range containers {
		if c.UserID > 0 {
			byUser[c.UserID] = c
		}
	}
	type userRow struct {
		ID          int64                       `json:"id"`
		Login       string                      `json:"login"`
		Role        string                      `json:"role"`
		CreatedAt   string                      `json:"createdAt"`
		Container   *dockerrt.ChatContainerInfo `json:"container,omitempty"`
		VolumeBytes int64                       `json:"volumeBytes"`
		Quota       interface{}                 `json:"quota"`
	}
	out := []userRow{}
	for _, u := range users {
		row := userRow{ID: u.ID, Login: u.Login, Role: u.Role, CreatedAt: u.CreatedAt}
		if c, ok := byUser[u.ID]; ok {
			row.Container = &c
		}
		if withSizes {
			row.VolumeBytes = userVolumeUsage(s.cfg.Home, u.ID)
		}
		if q, err := s.db.QuotaEffectiveForUser(u.Role, u.Login); err == nil && q.ID != 0 {
			row.Quota = q
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// handleAdminContainers — GET /api/admin/containers: все чат-контейнеры
// agctl со статами потребления (для running).
func (s *Server) handleAdminContainers(w http.ResponseWriter, r *http.Request, _ identity) {
	list, ok := s.listChatContainers()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "docker-рантайм недоступен")
		return
	}
	type row struct {
		dockerrt.ChatContainerInfo
		MemBytes   uint64  `json:"memBytes"`
		CPUPercent float64 `json:"cpuPercent"`
	}
	out := []row{}
	for _, c := range list {
		r0 := row{ChatContainerInfo: c}
		if c.State == "running" {
			if mem, cpu, ok := s.containerStats(c.ID); ok {
				r0.MemBytes, r0.CPUPercent = mem, cpu
			}
		}
		out = append(out, r0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": out})
}

// adminContainerFromPath — контейнер по имени из пути: имя обязано быть из
// списка agctl-чатов (защита от управления посторонними контейнерами).
func (s *Server) adminContainerFromPath(r *http.Request) (dockerrt.ChatContainerInfo, bool) {
	name := r.PathValue("name")
	if name == "" || !strings.HasPrefix(name, "agctl-") {
		return dockerrt.ChatContainerInfo{}, false
	}
	list, ok := s.listChatContainers()
	if !ok {
		return dockerrt.ChatContainerInfo{}, false
	}
	for _, c := range list {
		if c.Name == name {
			return c, true
		}
	}
	return dockerrt.ChatContainerInfo{}, false
}

// handleAdminContainerStop — POST /api/admin/containers/{name}/stop:
// остановить и удалить compute-контейнер (данные на томе остаются; следующий
// вход пользователя пересоздаст контейнер через ensureChat).
func (s *Server) handleAdminContainerStop(w http.ResponseWriter, r *http.Request, id identity) {
	c, ok := s.adminContainerFromPath(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "контейнер не найден или docker недоступен")
		return
	}
	if err := s.rt.Stop(r.Context(), c.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.evictChatByUser(c.UserID)
	_ = s.db.Audit(id.Login, "admin.container.stop", c.Name, "runtime удалён, том сохранён")
	writeJSON(w, http.StatusOK, map[string]any{"status": "stopped", "name": c.Name})
}

// handleAdminContainerStart — POST /api/admin/containers/{name}/start:
// поднять runtime пользователя заново (ensureChat идемпотентен по RuntimeID).
func (s *Server) handleAdminContainerStart(w http.ResponseWriter, r *http.Request, id identity) {
	c, ok := s.adminContainerFromPath(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "контейнер не найден или docker недоступен")
		return
	}
	if c.UserID <= 0 {
		writeErr(w, http.StatusBadRequest, "не удалось определить пользователя контейнера")
		return
	}
	if _, err := s.ensureChat(c.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "admin.container.start", c.Name, "runtime поднят")
	writeJSON(w, http.StatusOK, map[string]any{"status": "running", "name": c.Name})
}

// handleAdminContainerRestart — POST /api/admin/containers/{name}/restart:
// stop (удалить compute) + ensureChat (свежий контейнер на том же томе).
func (s *Server) handleAdminContainerRestart(w http.ResponseWriter, r *http.Request, id identity) {
	c, ok := s.adminContainerFromPath(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "контейнер не найден или docker недоступен")
		return
	}
	if c.UserID <= 0 {
		writeErr(w, http.StatusBadRequest, "не удалось определить пользователя контейнера")
		return
	}
	if err := s.rt.Stop(r.Context(), c.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.evictChatByUser(c.UserID)
	if _, err := s.ensureChat(c.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "admin.container.restart", c.Name, "runtime пересоздан на том же томе")
	writeJSON(w, http.StatusOK, map[string]any{"status": "restarted", "name": c.Name})
}

// handleAdminContainerLogs — GET /api/admin/containers/{name}/logs?tail=:
// хвост логов чат-контейнера (админ-панель), санитизация как в launcher.
func (s *Server) handleAdminContainerLogs(w http.ResponseWriter, r *http.Request, _ identity) {
	c, ok := s.adminContainerFromPath(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "контейнер не найден или docker недоступен")
		return
	}
	tail := atoiDefault(r.URL.Query().Get("tail"), 200)
	if tail > maxLauncherLogTail {
		tail = maxLauncherLogTail
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	logs, err := s.rt.Logs(ctx, c.ID, strconv.Itoa(tail))
	if err != nil {
		logs = "(контейнер не отвечает: " + err.Error() + ")"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"container": c.Name,
		"state":     c.State,
		"logs":      redactWorkspaceLogs(logs),
	})
}

// handleAdminActivity — GET /api/admin/activity: временные ряды для графиков
// вкладки «Обзор» (запросы шлюза по часам/дням, топ пользователей, статусы,
// регистрации). Пустые бакеты досыпаются, чтобы линия/столбцы были сплошными.
func (s *Server) handleAdminActivity(w http.ResponseWriter, r *http.Request, _ identity) {
	hourly, err := s.db.ProxyRequestsHourly(24)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	daily, err := s.db.ProxyRequestsDaily(14)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	topUsers, err := s.db.ProxyRequestsTopUsers(7, 10)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	statuses, err := s.db.ProxyRequestsStatuses(7)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	registrations, err := s.db.UsersRegisteredDaily(14)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hourly":        fillHourBuckets(hourly, 24),
		"daily":         fillDayBuckets(daily, 14),
		"topUsers":      topUsers,
		"statuses":      statuses,
		"registrations": fillDayCounts(registrations, 14),
	})
}

// fillHourBuckets — непрерывный ряд последних hours часовых бакетов (UTC):
// отсутствующие часы становятся нулевыми точками.
func fillHourBuckets(points []store.ActivityPoint, hours int) []store.ActivityPoint {
	byKey := make(map[string]store.ActivityPoint, len(points))
	for _, p := range points {
		byKey[p.Bucket] = p
	}
	now := time.Now().UTC().Truncate(time.Hour)
	out := make([]store.ActivityPoint, 0, hours)
	for i := hours - 1; i >= 0; i-- {
		key := now.Add(-time.Duration(i) * time.Hour).Format("2006-01-02T15:00:00Z")
		if p, ok := byKey[key]; ok {
			out = append(out, p)
			continue
		}
		out = append(out, store.ActivityPoint{Bucket: key})
	}
	return out
}

// fillDayBuckets — непрерывный ряд последних days дневных бакетов (UTC).
func fillDayBuckets(points []store.ActivityPoint, days int) []store.ActivityPoint {
	byKey := make(map[string]store.ActivityPoint, len(points))
	for _, p := range points {
		byKey[p.Bucket] = p
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]store.ActivityPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		key := today.AddDate(0, 0, -i).Format("2006-01-02T00:00:00Z")
		if p, ok := byKey[key]; ok {
			out = append(out, p)
			continue
		}
		out = append(out, store.ActivityPoint{Bucket: key})
	}
	return out
}

// fillDayCounts — непрерывный ряд счётчиков по дням (регистрации).
func fillDayCounts(counts []store.DayCount, days int) []store.DayCount {
	byKey := make(map[string]store.DayCount, len(counts))
	for _, c := range counts {
		byKey[c.Day] = c
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]store.DayCount, 0, days)
	for i := days - 1; i >= 0; i-- {
		key := today.AddDate(0, 0, -i).Format("2006-01-02T00:00:00Z")
		if c, ok := byKey[key]; ok {
			out = append(out, c)
			continue
		}
		out = append(out, store.DayCount{Day: key})
	}
	return out
}

// handleAdminRequests — GET /api/admin/requests?user_id=&limit=: журнал
// запросов, проксированных шлюзом /chat/ (метаданные, не тела).
func (s *Server) handleAdminRequests(w http.ResponseWriter, r *http.Request, _ identity) {
	uid := int64(atoiDefault(r.URL.Query().Get("user_id"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 200)
	rows, err := s.db.ProxyRequestsList(uid, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": rows})
}

// handleAdminQuotasList — GET /api/admin/quotas.
func (s *Server) handleAdminQuotasList(w http.ResponseWriter, r *http.Request, _ identity) {
	quotas, err := s.db.QuotaList()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quotas": quotas})
}

// quotaPutReq — тело PUT /api/admin/quotas.
type quotaPutReq struct {
	Scope         string `json:"scope"` // role | user
	Subject       string `json:"subject"`
	DailyTokens   int64  `json:"dailyTokens"`
	MonthlyTokens int64  `json:"monthlyTokens"`
	Note          string `json:"note"`
}

// handleAdminQuotaPut — PUT /api/admin/quotas: создать/обновить квоту.
func (s *Server) handleAdminQuotaPut(w http.ResponseWriter, r *http.Request, id identity) {
	var req quotaPutReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Note = strings.TrimSpace(req.Note)
	if len(req.Note) > 500 {
		req.Note = req.Note[:500]
	}
	if err := s.db.QuotaUpsert(req.Scope, req.Subject, req.DailyTokens, req.MonthlyTokens, req.Note); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "admin.quota.set", req.Scope+":"+req.Subject,
		"daily="+strconv.FormatInt(req.DailyTokens, 10)+"; monthly="+strconv.FormatInt(req.MonthlyTokens, 10))
	quotas, _ := s.db.QuotaList()
	writeJSON(w, http.StatusOK, map[string]any{"quotas": quotas})
}

// handleAdminQuotaDelete — DELETE /api/admin/quotas/{id}.
func (s *Server) handleAdminQuotaDelete(w http.ResponseWriter, r *http.Request, id identity) {
	qid, err := pathInt64(r, "id")
	if err != nil || qid <= 0 {
		writeErr(w, http.StatusBadRequest, "неверный id квоты")
		return
	}
	if err := s.db.QuotaDelete(qid); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "admin.quota.delete", strconv.FormatInt(qid, 10), "")
	quotas, _ := s.db.QuotaList()
	writeJSON(w, http.StatusOK, map[string]any{"quotas": quotas})
}

// ── шлюз /chat/: гейт роли + журнал запросов ────────────────────────────────

// statusRecorder — запоминает статус и объём ответа прокси, транслируя
// Flush/Hijack в лежащий ниже ResponseWriter (Unwrap для http.ResponseController).
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (sw *statusRecorder) WriteHeader(code int) {
	if sw.status == 0 {
		sw.status = code
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusRecorder) Write(b []byte) (int, error) {
	if sw.status == 0 {
		sw.status = http.StatusOK
	}
	n, err := sw.ResponseWriter.Write(b)
	sw.bytes += int64(n)
	return n, err
}

func (sw *statusRecorder) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (sw *statusRecorder) Unwrap() http.ResponseWriter { return sw.ResponseWriter }

// auditedChat — обёртка chatProxy: (1) админу личный чат не выдаётся —
// редирект на /admin; (2) каждый запрос пишется в журнал proxy_requests
// (метаданные: метод, путь, статус, длительность, объём; тела не пишутся).
func (s *Server) auditedChat(w http.ResponseWriter, r *http.Request, id identity) {
	if id.Role == "admin" {
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
		} else {
			writeErr(w, http.StatusForbidden, "личный чат админу не выдаётся")
		}
		return
	}
	start := time.Now()
	sw := &statusRecorder{ResponseWriter: w}
	s.chatProxy(sw, r, id)
	// WS/SSE живут до закрытия соединения: ms тут — длина стрима, это тоже
	// полезные данные активности.
	path := r.URL.Path
	if len(path) > 300 {
		path = path[:300]
	}
	if err := s.db.ProxyRequestInsert(id.ID, id.Login, r.Method, path, sw.status,
		time.Since(start).Milliseconds(), sw.bytes); err != nil {
		log.Printf("[admin] журнал запросов: %v", err)
		return
	}
	// Периодическая обрезка журнала: каждые 500 записей держим последние 5000.
	if s.proxyAuditN.Add(1)%500 == 0 {
		if removed, err := s.db.ProxyRequestsPrune(5000); err != nil {
			log.Printf("[admin] обрезка журнала запросов: %v", err)
		} else if removed > 0 {
			log.Printf("[admin] журнал запросов обрезан: удалено %d старых записей", removed)
		}
	}
}

// ── страницы ────────────────────────────────────────────────────────────────

// adminPageHandler — выдаёт index.html с брендингом на /admin (и /launcher
// в будущей итерации). Роль определяет сама SPA по /api/me; сервер лишь
// не кэширует, чтобы смена роли сразу меняла рендер.
func (s *Server) adminPageHandler(webFS fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.Error(w, "фронтенд не собран: выполните make web_build", http.StatusServiceUnavailable)
			return
		}
		index = injectBrandBoot(index, s.brand)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}
}

// decodeJSONBody — строгое чтение JSON-тела (пустое тело = ошибка).
func decodeJSONBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
