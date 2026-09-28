// Package store — SQLite-хранилище agctl (modernc.org/sqlite, pure Go,
// без cgo — кроссплатформенность mac/linux/windows). Миграции — встроенные
// SQL-файлы, применяются по PRAGMA user_version при открытии.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB — обёртка над sql.DB с миграциями.
type DB struct {
	*sql.DB
}

// Open открывает (создавая при необходимости) базу и накатывает миграции.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// sqlite + database/sql: одно соединение достаточно и безопаснее для WAL-записи.
	db.SetMaxOpenConns(1)
	d := &DB{DB: db}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	var version int
	if err := d.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for i, name := range names {
		v := i + 1
		if v <= version {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
			tx.Rollback()
			return fmt.Errorf("set user_version %d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// now возвращает UTC-строку того же формата, что datetime('now') в sqlite.
func now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }
