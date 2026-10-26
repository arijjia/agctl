package main

import (
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/store"
)

func TestSeedDemoAccountsIsExplicitAndIdempotent(t *testing.T) {
	home := t.TempDir()
	if _, err := seedDemoAccounts(home); err == nil {
		t.Fatal("seed before first registration must preserve first-user-is-admin")
	}
	db, err := store.Open(filepath.Join(home, "agctl.db"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("primary-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser("primary-admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	created, err := seedDemoAccounts(home)
	if err != nil {
		t.Fatal(err)
	}
	if created != len(demoAccounts) {
		t.Fatalf("created = %d, want %d", created, len(demoAccounts))
	}
	created, err = seedDemoAccounts(home)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("second seed created %d users, want none", created)
	}

	db, err = store.Open(filepath.Join(home, "agctl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, want := range demoAccounts {
		got, err := db.UserByLogin(want.login)
		if err != nil {
			t.Fatal(err)
		}
		if got.Role != want.role {
			t.Errorf("%s role = %s, want %s", want.login, got.Role, want.role)
		}
	}
}
