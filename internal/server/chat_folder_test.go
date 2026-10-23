package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatFolderInstructionsPreserveUserInstructions(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	sessionID := "session-8a6c4fe1-1eed-4cd8-93ce-54542017176c"
	dir := filepath.Join(e.home, "chats", "u1", "work", ".sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "AGENTS.local.md")
	if err := os.WriteFile(path, []byte("User's own instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, body := e.do(e.admin, "PATCH", "/api/chat/sessions/"+sessionID+"/folder-instructions", map[string]string{
		"folder": "Работа", "instructions": "Всегда сначала опиши план, затем вноси изменения.",
	})
	if code != 200 || !strings.Contains(body, `"updated":true`) {
		t.Fatalf("save folder instructions: %d %s", code, body)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "User's own instructions.") || !strings.Contains(string(got), "Всегда сначала опиши план") || !strings.Contains(string(got), "Папка «Работа»") {
		t.Fatalf("user/folder instructions not merged: %s", got)
	}

	code, body = e.do(e.admin, "PATCH", "/api/chat/sessions/"+sessionID+"/folder-instructions", map[string]string{
		"folder": "Работа", "instructions": "Сначала проверь тесты.",
	})
	if code != 200 {
		t.Fatalf("update folder instructions: %d %s", code, body)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "Всегда сначала опиши план") || !strings.Contains(string(got), "Сначала проверь тесты") || strings.Count(string(got), folderInstructionStart) != 1 {
		t.Fatalf("managed block wasn't replaced atomically: %s", got)
	}

	code, body = e.do(e.admin, "PATCH", "/api/chat/sessions/"+sessionID+"/folder-instructions", map[string]string{"folder": "", "instructions": ""})
	if code != 200 {
		t.Fatalf("remove folder instructions: %d %s", code, body)
	}
	got, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), "User's own instructions.") || strings.Contains(string(got), folderInstructionStart) {
		t.Fatalf("remove should preserve user instructions: %q err=%v", got, err)
	}
}

func TestChatFolderInstructionsPrecreateNewSessionWorkspace(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	workRoot := filepath.Join(e.home, "chats", "u1", "work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "session-new-uuid-1234"
	code, body := e.do(e.admin, "PATCH", "/api/chat/sessions/"+sessionID+"/folder-instructions", map[string]string{
		"folder": "Работа", "instructions": "Перед ответом составь план.",
	})
	if code != 200 {
		t.Fatalf("precreate session instructions: %d %s", code, body)
	}
	path := filepath.Join(e.home, "chats", "u1", "work", ".sessions", sessionID, "AGENTS.local.md")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "Перед ответом составь план") {
		t.Fatalf("precreated instructions file missing: %q err=%v", data, err)
	}
}

func TestChatFolderInstructionsRejectInvalidSessionIDs(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	for _, sessionID := range []string{"session-bad_name", "not-a-session"} {
		code, _ := e.do(e.admin, "PATCH", "/api/chat/sessions/"+sessionID+"/folder-instructions", map[string]string{"folder": "x", "instructions": "y"})
		if code != 400 {
			t.Fatalf("session ID %q status=%d, want 400", sessionID, code)
		}
	}
}

func TestChatFoldersStatePersistsPerUser(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "GET", "/api/chat/folders", nil)
	if code != 200 || !strings.Contains(body, `"folders":[]`) {
		t.Fatalf("initial folders: %d %s", code, body)
	}
	state := map[string]any{
		"folders":  []string{"Работа"},
		"active":   "Работа",
		"assign":   map[string]string{"session-8a6c4fe1-1eed-4cd8-93ce-54542017176c": "Работа"},
		"settings": map[string]any{"Работа": map[string]string{"instructions": "Сначала план."}},
	}
	if code, body = e.do(e.admin, "PUT", "/api/chat/folders", state); code != 200 {
		t.Fatalf("save folders: %d %s", code, body)
	}
	code, body = e.do(e.admin, "GET", "/api/chat/folders", nil)
	if code != 200 || !strings.Contains(body, "Сначала план") || !strings.Contains(body, "session-8a6c4fe1") {
		t.Fatalf("folder state not persisted: %d %s", code, body)
	}
	invalid := map[string]any{"folders": []string{"bad\nname"}, "assign": map[string]string{}, "settings": map[string]any{}}
	if code, body = e.do(e.admin, "PUT", "/api/chat/folders", invalid); code != 400 {
		t.Fatalf("invalid folder name status=%d body=%s", code, body)
	}
}
