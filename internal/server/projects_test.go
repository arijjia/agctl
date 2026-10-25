package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
)

func TestProjectsAPIAuthenticationAndIsolation(t *testing.T) {
	e := newTestEnv(t, 3)
	for _, method := range []string{"GET", "POST"} {
		if code, _ := e.do(e.newClient(), method, "/api/projects", map[string]string{"name": "demo"}); code != 401 {
			t.Fatalf("%s unauthenticated: %d", method, code)
		}
	}
	e.register(e.admin, "anna", "secret1")
	if code, body := e.do(e.admin, "GET", "/api/projects", nil); code != 200 || !strings.Contains(body, `"projects":[]`) {
		t.Fatalf("empty %d %s", code, body)
	}
	code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "Мой проект"})
	if code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	var response struct {
		Project struct{ Name, Directory string }
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	if response.Project.Directory != "/workspace/Мой проект" || strings.Contains(body, e.home) {
		t.Fatal(body)
	}
	if _, err := git.PlainOpen(filepath.Join(e.home, "chats", "u1", "work", "Мой проект")); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "Мой проект"}); code != 409 {
		t.Fatalf("duplicate %d %s", code, body)
	}
	other := e.newClient()
	e.register(other, "boris", "secret2")
	if code, body := e.do(other, "GET", "/api/projects", nil); code != 200 || !strings.Contains(body, `"projects":[]`) {
		t.Fatalf("isolation %d %s", code, body)
	}
	if code, body := e.do(other, "POST", "/api/projects", map[string]string{"name": "Мой проект"}); code != 201 {
		t.Fatalf("other user %d %s", code, body)
	}
}

func TestProjectsAPIRejectsUnsafeInput(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	for _, name := range []string{"../escape", "/tmp/outside", `x\y`, ".ssh", ".env", "", "x\n"} {
		if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": name}); code != 400 {
			t.Fatalf("%q %d %s", name, code, body)
		}
	}
	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]any{"name": "demo", "userID": 2}); code != 400 {
		t.Fatalf("extra fields %d %s", code, body)
	}
	root := filepath.Join(e.home, "chats", "u1", "work")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "linked"}); code != 409 {
		t.Fatalf("link %d %s", code, body)
	}
	if code, body := e.do(e.admin, "GET", "/api/projects", nil); code != 200 || strings.Contains(body, "linked") {
		t.Fatalf("link listed %d %s", code, body)
	}
}

func TestProjectsAutosaveIncludesSleepingChats(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	other := e.newClient()
	e.register(other, "boris", "secret2")
	for i, client := range []*http.Client{e.admin, other} {
		for _, name := range []string{"first", "second"} {
			if code, body := e.do(client, "POST", "/api/projects", map[string]string{"name": name}); code != 201 {
				t.Fatalf("%d %s", code, body)
			}
			path := filepath.Join(e.home, "chats", fmt.Sprintf("u%d", i+1), "work", name, "work.txt")
			if err := os.WriteFile(path, []byte("saved locally"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(e.srv.chats) != 0 {
		t.Fatal("API started chat containers")
	}
	e.srv.autosaveLocalProjects()
	for _, user := range []string{"u1", "u2"} {
		for _, name := range []string{"first", "second"} {
			repo, err := git.PlainOpen(filepath.Join(e.home, "chats", user, "work", name))
			if err != nil {
				t.Fatal(err)
			}
			head, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}
			commit, err := repo.CommitObject(head.Hash())
			if err != nil {
				t.Fatal(err)
			}
			file, err := commit.File("work.txt")
			if err != nil {
				t.Fatal("not autosaved", err)
			}
			contents, err := file.Contents()
			if err != nil || contents != "saved locally" {
				t.Fatalf("%q %v", contents, err)
			}
			remotes, err := repo.Remotes()
			if err != nil || len(remotes) != 0 {
				t.Fatal("non-local repo", err)
			}
		}
	}
}

func TestProjectsCloneURLPolicy(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	env2 := e.newClient()
	e.register(env2, "bob", "secret2")

	// user: клонирование — developer-настройка.
	if code, body := e.do(env2, "POST", "/api/projects", map[string]string{
		"name": "bob-clone", "cloneUrl": "https://github.com/u/r.git",
	}); code != http.StatusForbidden {
		t.Fatalf("user clone = %d %s, want 403", code, body)
	}
	// Небезопасные схемы отклоняются до сети.
	for _, raw := range []string{"file:///etc", "ssh://git@host/x", "https://"} {
		if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{
			"name": "adm-" + strings.SplitN(raw, ":", 1)[0], "cloneUrl": raw,
		}); code != http.StatusBadRequest {
			t.Fatalf("clone %q = %d %s, want 400", raw, code, body)
		}
	}
	// Обычное создание без cloneUrl продолжает работать.
	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "plain"}); code != http.StatusCreated {
		t.Fatalf("plain create = %d %s", code, body)
	}
}
