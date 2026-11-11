package server

// chat_prewarm_test.go — prewarm-пул личных чатов (карточка
// chat-prewarm-pool): подогрев по расписанию/логину, потолок пула,
// per-user TTL, квота тома, метрики hit-rate.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// prewarmSeedActivity регистрирует logins и кладёт активность в журнал
// шлюза: чем меньше idx, тем старее активность (свежесть — по ts).
func prewarmSeedActivity(t *testing.T, e *testEnv, logins []string) []int64 {
	t.Helper()
	ids := make([]int64, len(logins))
	for i, login := range logins {
		if code := e.register(e.admin, login, "secret1"); code >= 300 {
			t.Fatalf("register %s: %d", login, code)
		}
		u, err := e.db.UserByLogin(login)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = u.ID
		if err := e.db.ProxyRequestInsert(u.ID, login, "GET", "/chat/", 200, 1, 1); err != nil {
			t.Fatal(err)
		}
		// SQLite-журнал хранит секунды: расставляем порядок явными ts.
		ts := time.Now().UTC().Add(-time.Duration(len(logins)-i) * time.Hour)
		if _, err := e.db.Exec(`UPDATE proxy_requests SET ts = ? WHERE user_id = ?`,
			ts.UTC().Format("2006-01-02 15:04:05"), u.ID); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func TestPrewarmPoolWarmsRecentUsersUpToLimit(t *testing.T) {
	e := newTestEnvWith(t, 5, func(c *Config) {
		c.ChatPrewarmN = 2
		c.ChatPrewarmEvery = time.Hour // тикет не стреляет — зовём prewarmOnce руками
	})
	ids := prewarmSeedActivity(t, e, []string{"old-user", "fresh-user", "newest-user"})

	e.srv.prewarmOnce(nil)
	if got := e.srv.warmCount(); got != 2 {
		t.Fatalf("warm count = %d, want 2 (лимит пула)", got)
	}
	// Свежие по журналу: newest-user и fresh-user; old-user остаётся холодным.
	for _, uid := range ids[1:] {
		if e.srv.chats[uid] == nil {
			t.Fatalf("пользователь %d не подогрет", uid)
		}
	}
	if e.srv.chats[ids[0]] != nil {
		t.Fatal("пул разогрел пользователя за пределами лимита")
	}
	if e.srv.coldStarts.Load() != 2 {
		t.Fatalf("coldStarts = %d, want 2", e.srv.coldStarts.Load())
	}
	// Повторный цикл — no-op: все свежие уже тёплые.
	e.srv.prewarmOnce(nil)
	if e.srv.coldStarts.Load() != 2 || e.srv.warmCount() != 2 {
		t.Fatal("повторный цикл подогрел лишнее")
	}
}

func TestPrewarmDisabledByDefault(t *testing.T) {
	e := newTestEnv(t, 3)
	prewarmSeedActivity(t, e, []string{"someone"})
	e.srv.prewarmOnce(nil)
	if got := e.srv.warmCount(); got != 0 {
		t.Fatalf("warm count = %d, want 0 (пул выключен)", got)
	}
}

func TestPrewarmOnLogin(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) {
		c.ChatPrewarmN = 1
		c.ChatPrewarmEvery = time.Hour
	})
	ids := prewarmSeedActivity(t, e, []string{"sleepy"})
	c := e.newClient()
	if code, body := e.do(c, "POST", "/api/login", map[string]string{"login": "sleepy", "password": "secret1"}); code != http.StatusOK {
		t.Fatalf("login = %d %s", code, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.srv.warmCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.srv.chats[ids[0]] == nil {
		t.Fatal("логин не подогрел среду пользователя")
	}
}

func TestPerUserChatTTLOverride(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Hour // серверный дефолт — час
	ids := prewarmSeedActivity(t, e, []string{"short-ttl"})
	if err := e.db.SettingSet(fmt.Sprintf("chat-ttl-min-u%d", ids[0]), "1"); err != nil {
		t.Fatal(err)
	}
	chat, err := e.srv.ensureChat(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	// 2 минуты простоя: серверного TTL (час) мало, персонального (минута) хватает.
	chat.lastSeen = time.Now().Add(-2 * time.Minute)
	e.srv.reapIdleChats()
	if e.srv.chats[ids[0]] != nil {
		t.Fatal("персональный TTL не усыпил runtime")
	}
	// Мусор в настройке — серверный дефолт, не падаем.
	if err := e.db.SettingSet(fmt.Sprintf("chat-ttl-min-u%d", ids[0]), "abc"); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.userChatTTL(ids[0]); got != time.Hour {
		t.Fatalf("userChatTTL(мусор) = %v, want серверский дефолт", got)
	}
}

func TestEnsureChatWarmHitMetrics(t *testing.T) {
	e := newTestEnv(t, 3)
	ids := prewarmSeedActivity(t, e, []string{"metric"})
	if _, err := e.srv.ensureChat(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.ensureChat(ids[0]); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.warmHits.Load(); got != 1 {
		t.Fatalf("warmHits = %d, want 1", got)
	}
	if got := e.srv.coldStarts.Load(); got != 1 {
		t.Fatalf("coldStarts = %d, want 1", got)
	}
}

func TestVolumeQuotaBlocksProjectCreate(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.VolumeQuotaGB = 0.000001 }) // ~1 КБ
	ids := prewarmSeedActivity(t, e, []string{"full-disk"})
	// Том уже «занят»: кладём файл в персональный каталог пользователя.
	vol := filepath.Join(e.home, "chats", fmt.Sprintf("u%d", ids[0]), "dsh-home")
	if err := os.MkdirAll(vol, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vol, "big.bin"), make([]byte, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	c := e.newClient()
	if code, body := e.do(c, "POST", "/api/login", map[string]string{"login": "full-disk", "password": "secret1"}); code != http.StatusOK {
		t.Fatalf("login = %d %s", code, body)
	}
	if code, body := e.do(c, "POST", "/api/projects", map[string]string{"name": "demo"}); code != http.StatusForbidden {
		t.Fatalf("create = %d %s, want 403 по квоте", code, body)
	}
	// Персональное отключение квоты возвращает создание.
	if err := e.db.SettingSet(fmt.Sprintf("volume-quota-gb-u%d", ids[0]), "0"); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(c, "POST", "/api/projects", map[string]string{"name": "demo"}); code != http.StatusCreated {
		t.Fatalf("create после снятия квоты = %d %s", code, body)
	}
}

func TestAdminUserLimitsAPI(t *testing.T) {
	e := newTestEnv(t, 3)
	ids := prewarmSeedActivity(t, e, []string{"limited"})
	path := fmt.Sprintf("/api/admin/users/%d/limits", ids[0])
	if code, body := e.do(e.admin, "PUT", path, map[string]any{"chatTTLMin": 5, "volumeQuotaGB": 1.5}); code != http.StatusOK {
		t.Fatalf("limits = %d %s", code, body)
	}
	if got := e.srv.userChatTTL(ids[0]); got != 5*time.Minute {
		t.Fatalf("userChatTTL = %v, want 5m", got)
	}
	if got := e.srv.userVolumeQuotaGB(ids[0]); got != 1.5 {
		t.Fatalf("userVolumeQuotaGB = %v, want 1.5", got)
	}
	// volumeQuotaGB=-1: персонально выключена.
	if code, body := e.do(e.admin, "PUT", path, map[string]any{"volumeQuotaGB": -1}); code != http.StatusOK {
		t.Fatalf("limits(-1) = %d %s", code, body)
	}
	if got := e.srv.userVolumeQuotaGB(ids[0]); got != 0 {
		t.Fatalf("userVolumeQuotaGB(-1) = %v, want 0", got)
	}
	// Мусор отклоняется.
	if code, _ := e.do(e.admin, "PUT", path, map[string]any{"chatTTLMin": -5}); code != http.StatusBadRequest {
		t.Fatalf("negative ttl = %d, want 400", code)
	}
}
