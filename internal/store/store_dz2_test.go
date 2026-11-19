package store

// store_dz2_test.go — юнит-охота домашнего задания РТиППО (ветка
// agent/dz2-unit-coverage): покрытие незакрытых функций хранилища и проверка
// гипотезы ИИ-аудита об отсутствии гвардии pending у workspace-заявок.
import (
	"errors"
	"testing"
)

func TestDZ2SettingsUpsertRoundTrip(t *testing.T) {
	db := openTest(t)
	if err := db.SettingSet("dz2", "v1"); err != nil {
		t.Fatalf("первый Set: %v", err)
	}
	if v, _ := db.SettingGet("dz2"); v != "v1" {
		t.Fatalf("после первого Set получили %q", v)
	}
	// Повторная установка той же позиции — должна обновлять, а не падать.
	if err := db.SettingSet("dz2", "v2"); err != nil {
		t.Fatalf("повторный Set (upsert): %v", err)
	}
	if v, _ := db.SettingGet("dz2"); v != "v2" {
		t.Fatalf("после повторного Set получили %q, ждали v2", v)
	}
}

func TestDZ2UsersListAndByID(t *testing.T) {
	db := openTest(t)
	id1, _ := db.CreateUser("dz2_a", "hash-a", "user")
	id2, _ := db.CreateUser("dz2_b", "hash-b", "user")
	users, err := db.UsersList()
	if err != nil {
		t.Fatalf("UsersList: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("UsersList вернул %d записей, ждали 2", len(users))
	}
	u, err := db.UserByID(id1)
	if err != nil || u.Login != "dz2_a" {
		t.Fatalf("UserByID: %v, %q", err, u.Login)
	}
	if _, err := db.UserByID(id2 + 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UserByID несуществующего: %v, ждали ErrNotFound", err)
	}
}
