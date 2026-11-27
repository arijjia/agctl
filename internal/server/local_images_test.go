package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// waitImageTask — фоновая операция раздела «Образы» закончилась.
func waitImageTask(t *testing.T, env *testEnv) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := env.srv.imageTask().snapshot()
		if snap["state"] != "building" {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("операция с образом не закончилась: %v", env.srv.imageTask().snapshot())
	return nil
}

// Раздел «Образы»: каталог с местами, которые используют образ, план
// сборки, действия; образ работающего места не удаляется и не пересобирается.
func TestLocalImages(t *testing.T) {
	env, h := newLocalUIEnv(t)
	c := loginOwner(t, env)
	code, body := env.do(c, "GET", "/api/local/ui/images", nil)
	if code != 200 || !strings.Contains(body, `"actions":true`) || !strings.Contains(body, `"ref":"agctl-dsh-org:0.2.2"`) {
		t.Fatalf("список: %d %s", code, body)
	}
	if !strings.Contains(body, `"usedBy":[{"id":"main","name":"Основное","running":false}]`) || !strings.Contains(body, `"current":true`) {
		t.Fatalf("основное место использует образ по умолчанию: %s", body)
	}
	code, body = env.do(c, "GET", "/api/local/ui/images/plan?ref=agctl-dsh-org:0.2.2", nil)
	if code != 200 || !strings.Contains(body, `"kind":"profile"`) || !strings.Contains(body, `"present":true`) {
		t.Fatalf("план: %d %s", code, body)
	}
	if code, _ := env.do(c, "GET", "/api/local/ui/images/plan", nil); code != http.StatusBadRequest {
		t.Fatalf("план без ref: %d", code)
	}

	// работающее основное место — его образ не трогаем
	if code, _ := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatal("старт основного")
	}
	env.waitTask("idle")
	if code, body := env.do(c, "POST", "/api/local/ui/images/remove", map[string]string{"ref": "agctl-dsh-org:0.2.2"}); code != http.StatusConflict || !strings.Contains(body, "Основное") {
		t.Fatalf("удаление образа работающего места: %d %s", code, body)
	}

	// недостающий образ — скачать или собрать
	if code, body := env.do(c, "POST", "/api/local/ui/images/ensure", map[string]string{"ref": "agctl-dsh-org-1c:0.2.2"}); code != http.StatusAccepted {
		t.Fatalf("ensure: %d %s", code, body)
	}
	if snap := waitImageTask(t, env); snap["state"] != "idle" || !strings.Contains(strings.Join(snap["log"].([]string), "\n"), "готов") {
		t.Fatalf("ensure: %v", snap)
	}
	// пересобрать не используемый работающим местом
	if code, body := env.do(c, "POST", "/api/local/ui/images/rebuild", map[string]string{"ref": "agctl-dsh-org-1c:0.2.2"}); code != http.StatusAccepted {
		t.Fatalf("rebuild: %d %s", code, body)
	}
	waitImageTask(t, env)
	h.mu.Lock()
	removed, built := strings.Join(h.removed, ","), strings.Join(h.built, ",")
	h.mu.Unlock()
	if removed != "agctl-dsh-org-1c:0.2.2" || built != "agctl-dsh-org-1c:0.2.2,agctl-dsh-org-1c:0.2.2" {
		t.Fatalf("пересборка: удалены %q, собраны %q", removed, built)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/images/explode", map[string]string{"ref": "x:1"}); code != http.StatusNotFound {
		t.Fatalf("неизвестное действие: %d", code)
	}
}
