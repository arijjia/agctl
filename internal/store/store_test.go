package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUsersAndSessions(t *testing.T) {
	db := openTest(t)
	if n, _ := db.CountUsers(); n != 0 {
		t.Fatalf("users = %d, ждали 0", n)
	}
	id, err := db.CreateUser("anna", "hash1", "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.CreateUser("anna", "hash2", "user"); err == nil {
		t.Fatal("дубликат логина должен быть ошибкой")
	}
	u, err := db.UserByLogin("anna")
	if err != nil || u.Role != "admin" {
		t.Fatalf("user by login: %v %+v", err, u)
	}
	if err := db.CreateSession("tok123", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("session: %v", err)
	}
	su, err := db.SessionUser("tok123")
	if err != nil || su.Login != "anna" {
		t.Fatalf("session user: %v", err)
	}
	if err := db.CreateSession("expired", id, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser("expired"); err == nil {
		t.Fatal("просроченная сессия должна не находиться")
	}
	_ = db.DeleteExpiredSessions()
	if _, err := db.SessionUser("tok123"); err != nil {
		t.Fatalf("живая сессия пропала: %v", err)
	}
	_ = db.DeleteSession("tok123")
	if _, err := db.SessionUser("tok123"); err == nil {
		t.Fatal("сессия не удалена")
	}
}

func TestWorkspacesCRUD(t *testing.T) {
	db := openTest(t)
	uid, _ := db.CreateUser("anna", "h", "user")
	ws := Workspace{
		ID: "w-1", OwnerID: uid, Name: "demo", Status: "provisioning",
		RepoPath: "/x/demo.git", WorkPath: "/w/w-1/repo", DshHome: "/w/w-1/dsh-home",
		HostPort: 50000, BaseURL: "https://llm/v1", Model: "m1",
	}
	if err := db.InsertWorkspace(ws); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertWorkspace(ws); err == nil {
		t.Fatal("дубликат (owner,name) должен быть ошибкой")
	}
	if err := db.UpdateWorkspaceRuntime("w-1", "cid42", 50000, "running"); err != nil {
		t.Fatalf("runtime: %v", err)
	}
	got, err := db.WorkspaceByID("w-1")
	if err != nil || got.Status != "running" || got.ContainerID != "cid42" {
		t.Fatalf("got: %+v err=%v", got, err)
	}
	all, err := db.WorkspacesAll()
	if err != nil || len(all) != 1 {
		t.Fatalf("all: %v len=%d", err, len(all))
	}
	_ = db.DeleteWorkspace("w-1")
	if _, err := db.WorkspaceByID("w-1"); err != ErrNotFound {
		t.Fatalf("ждали ErrNotFound, получили %v", err)
	}
}

func TestSecretsAndAudit(t *testing.T) {
	db := openTest(t)
	uid, _ := db.CreateUser("anna", "h", "user")
	if err := db.SecretUpsert(uid, "https://llm/v1", "m1", "cipher1"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.SecretUpsert(uid, "https://llm2/v1", "m2", "cipher2"); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	s, err := db.SecretByUser(uid)
	if err != nil || s.Ciphertext != "cipher2" || s.Model != "m2" {
		t.Fatalf("secret: %+v err=%v", s, err)
	}
	_ = db.Audit("anna", "login", "anna", "")
	_ = db.Audit("anna", "ws.create", "w-1", "x")
	ev, err := db.AuditList(10)
	if err != nil || len(ev) != 2 || ev[0].Action != "ws.create" {
		t.Fatalf("audit: %+v err=%v", ev, err)
	}
}
