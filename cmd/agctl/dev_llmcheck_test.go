package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Проверка модели: вызов инструмента, ответ текстом, отказ шлюза на
// tool_choice=auto при живом чате, список моделей; ключ не печатается.
func TestLLMProbe(t *testing.T) {
	const key = "sk-secret-123456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"tools"},{"id":"text"},{"id":"chat-only"}]}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, withTools := body["tools"]
		switch body["model"] {
		case "tools":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"get_time","arguments":"{}"}}]}}]}`))
		case "text":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"12:00"}}]}`))
		case "chat-only":
			if withTools {
				http.Error(w, `{"error":"\"auto\" tool choice requires --enable-auto-tool-choice and --tool-call-parser to be set (key `+key+`)"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ок"}}]}`))
		default:
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := &http.Client{Timeout: 5 * time.Second}
	base := srv.URL + "/v1"
	if r := llmProbe(c, base, key, "tools"); !r.ToolsOK {
		t.Fatalf("tools: %+v", r)
	}
	if r := llmProbe(c, base, key, "text"); r.ToolsOK || !r.TextOnly {
		t.Fatalf("text: %+v", r)
	}
	r := llmProbe(c, base, key, "chat-only")
	if r.ToolsOK || !r.ChatOK || !strings.Contains(r.ToolsErr, "enable-auto-tool-choice") {
		t.Fatalf("chat-only: %+v", r)
	}
	if s := llmHideKey(r.String(), key); strings.Contains(s, key) || !strings.Contains(s, "<ключ>") {
		t.Fatalf("ключ в выводе: %s", s)
	}
	if r := llmProbe(c, base, key, "nope"); r.ChatOK || !strings.Contains(r.String(), "не отвечает") {
		t.Fatalf("nope: %+v", r)
	}
	ids, err := llmListModels(c, base, key)
	if err != nil || strings.Join(ids, ",") != "chat-only,text,tools" {
		t.Fatalf("models: %v %v", ids, err)
	}
	if _, err := llmListModels(c, base, "wrong"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("чужой ключ: %v", err)
	}
}
