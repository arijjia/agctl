package server

import (
	"errors"
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
	// пересобрать не используемый работающим местом — на этой машине, без реестра
	if code, body := env.do(c, "POST", "/api/local/ui/images/rebuild", map[string]string{"ref": "agctl-dsh-org-1c:0.2.2"}); code != http.StatusAccepted {
		t.Fatalf("rebuild: %d %s", code, body)
	}
	if snap := waitImageTask(t, env); !strings.Contains(strings.Join(snap["log"].([]string), "\n"), "собираю на машине agctl-dsh-org-1c:0.2.2") {
		t.Fatalf("rebuild не через сборку на машине: %v", snap)
	}
	h.mu.Lock()
	removed, built := strings.Join(h.removed, ","), strings.Join(h.built, ",")
	h.mu.Unlock()
	if removed != "agctl-dsh-org-1c:0.2.2" || built != "agctl-dsh-org-1c:0.2.2,agctl-dsh-org-1c:0.2.2" {
		t.Fatalf("пересборка: удалены %q, собраны %q", removed, built)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/images/explode", map[string]string{"ref": "x:1"}); code != http.StatusNotFound {
		t.Fatalf("неизвестное действие: %d", code)
	}
	// чужой образ не собрать — ошибка до удаления, образ цел
	if code, _ := env.do(c, "POST", "/api/local/ui/images/rebuild", map[string]string{"ref": "ubuntu:24.04"}); code != http.StatusAccepted {
		t.Fatal("rebuild чужого образа")
	}
	if snap := waitImageTask(t, env); snap["state"] != "error" || !strings.Contains(snap["error"].(string), "не собрать") {
		t.Fatalf("rebuild чужого образа: %v", snap)
	}
	h.mu.Lock()
	removed = strings.Join(h.removed, ",")
	h.mu.Unlock()
	if removed != "agctl-dsh-org-1c:0.2.2" {
		t.Fatalf("после отказа пересборки удалено лишнее: %q", removed)
	}
}

// «Скачать из реестра»: можно и для образа работающего места — новое
// применится при перезапуске (журнал и «нужен перезапуск»); не скачался —
// ошибка операции, без сборки.
func TestLocalImagePull(t *testing.T) {
	env, h := newLocalUIEnv(t)
	c := loginOwner(t, env)
	if code, _ := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatal("старт основного")
	}
	env.waitTask("idle")
	code, body := env.do(c, "POST", "/api/local/ui/images/pull", map[string]string{"ref": "agctl-dsh-org:0.2.2"})
	if code != http.StatusAccepted {
		t.Fatalf("pull образа работающего места: %d %s", code, body)
	}
	snap := waitImageTask(t, env)
	logText := strings.Join(snap["log"].([]string), "\n")
	if snap["state"] != "idle" || !strings.Contains(logText, "docker pull registry.test/agctl-dsh-org:0.2.2") ||
		!strings.Contains(logText, "«Основное»") || !strings.Contains(logText, "применится при перезапуске") {
		t.Fatalf("pull: %v", snap)
	}
	if main := env.srv.local.task.snapshot(); main["restartNeeded"] != true {
		t.Fatalf("работающее место не помечено «нужен перезапуск»: %v", main)
	}
	h.mu.Lock()
	pulled, built := strings.Join(h.pulled, ","), len(h.built)
	h.pullErr = errors.New("agctl-dsh-org-1c:0.2.2 из registry.test/agctl-dsh-org-1c:0.2.2 не скачался: в реестре нет такого образа")
	h.mu.Unlock()
	if pulled != "agctl-dsh-org:0.2.2" || built != 0 {
		t.Fatalf("pull: скачаны %q, собрано %d", pulled, built)
	}
	// реестр не отдал — ошибка операции, сборки нет
	if code, _ := env.do(c, "POST", "/api/local/ui/images/pull", map[string]string{"ref": "agctl-dsh-org-1c:0.2.2"}); code != http.StatusAccepted {
		t.Fatalf("pull: %d", code)
	}
	if snap := waitImageTask(t, env); snap["state"] != "error" || !strings.Contains(snap["error"].(string), "не скачался") {
		t.Fatalf("ошибка pull: %v", snap)
	}
	h.mu.Lock()
	built = len(h.built)
	h.mu.Unlock()
	if built != 0 {
		t.Fatal("после ошибки pull началась сборка")
	}
}

// Файлы контекста сборки: текст файла, список файлов каталога; двоичный —
// 422 с размером, нет такого — 404, путь с «..» и ступень не с 1 — 400.
func TestLocalImageFile(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	base := "/api/local/ui/images/file?ref=agctl-dsh-org:0.2.2&stage=1&path="
	code, body := env.do(c, "GET", base+"entrypoint.sh", nil)
	if code != 200 || !strings.Contains(body, `"text":"#!/bin/sh\necho hi\n"`) {
		t.Fatalf("файл: %d %s", code, body)
	}
	code, body = env.do(c, "GET", base+"skel/", nil)
	if code != 200 || !strings.Contains(body, `"dir":true`) || !strings.Contains(body, `"path":"skel/cordis.patch.yml"`) {
		t.Fatalf("каталог: %d %s", code, body)
	}
	if code, body := env.do(c, "GET", base+"agctl", nil); code != http.StatusUnprocessableEntity || !strings.Contains(body, "41.0 МБ") {
		t.Fatalf("двоичный: %d %s", code, body)
	}
	if code, _ := env.do(c, "GET", base+"nope.txt", nil); code != http.StatusNotFound {
		t.Fatalf("нет такого файла: %d", code)
	}
	if code, _ := env.do(c, "GET", "/api/local/ui/images/file?ref=agctl-dsh-org:0.2.2&stage=2&path=entrypoint.sh", nil); code != http.StatusNotFound {
		t.Fatalf("нет такой ступени: %d", code)
	}
	for _, bad := range []string{
		base + "../etc/passwd",
		base + "/etc/passwd",
		base,
		"/api/local/ui/images/file?ref=agctl-dsh-org:0.2.2&stage=0&path=entrypoint.sh",
		"/api/local/ui/images/file?ref=agctl-dsh-org:0.2.2&path=entrypoint.sh",
		"/api/local/ui/images/file?stage=1&path=entrypoint.sh",
	} {
		if code, _ := env.do(c, "GET", bad, nil); code != http.StatusBadRequest {
			t.Fatalf("%s: ждали 400, получили %d", bad, code)
		}
	}
}
