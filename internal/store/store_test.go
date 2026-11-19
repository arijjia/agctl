package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openTest — тестовая БД: sqlite по умолчанию; PostgreSQL при
// AGCTL_TEST_STORE=pg + AGCTL_TEST_PG_DSN (crosstest диалектов, карточка
// ha-control-plane п.2: PG перед телом теста получает чистую схему).
func openTest(t *testing.T) *DB {
	t.Helper()
	if os.Getenv("AGCTL_TEST_STORE") == "pg" {
		dsn := os.Getenv("AGCTL_TEST_PG_DSN")
		if dsn == "" {
			t.Skip("AGCTL_TEST_STORE=pg, но AGCTL_TEST_PG_DSN не задан")
		}
		raw, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("pg raw open: %v", err)
		}
		defer raw.Close()
		for _, stmt := range []string{
			`DROP SCHEMA public CASCADE`,
			`CREATE SCHEMA public`,
		} {
			if _, err := raw.Exec(stmt); err != nil {
				t.Fatalf("pg reset %q: %v", stmt, err)
			}
		}
		raw.Close()
		db, err := Open(dsn)
		if err != nil {
			t.Fatalf("open pg: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
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

func TestSecretsAndAudit(t *testing.T) {
	db := openTest(t)
	uid, _ := db.CreateUser("anna", "h", "user")
	if err := db.SecretUpsert(uid, "https://llm/v1", "m1", []string{"m1"}, "cipher1"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.SecretUpsert(uid, "https://llm2/v1", "m2", []string{"m2", "m3"}, "cipher2"); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	s, err := db.SecretByUser(uid)
	if err != nil || s.Ciphertext != "cipher2" || s.Model != "m2" || len(s.Models) != 2 {
		t.Fatalf("secret: %+v err=%v", s, err)
	}
	_ = db.Audit("anna", "login", "anna", "")
	_ = db.Audit("anna", "ws.create", "w-1", "x")
	ev, err := db.AuditList(10)
	if err != nil || len(ev) != 2 || ev[0].Action != "ws.create" {
		t.Fatalf("audit: %+v err=%v", ev, err)
	}
}
