// Package store — хранилище agctl: SQLite (modernc.org/sqlite, pure Go,
// по умолчанию) или PostgreSQL (pgx/v5 stdlib) — DSN с префиксом
// postgres://. Карточка ha-control-plane, план п.2: абстракция store —
// диалектные места (strftime/datetime/PRAGMA/LastInsertId) вынесены в
// хелперы ниже; схема миграций общая (TEXT-таймстампы — лексические
// сравнения одинаковы в обоих диалектах). Миграции — встроенные SQL-файлы,
// применяются при открытии (sqlite: PRAGMA user_version; pg: таблица
// schema_migrations).
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB — обёртка над sql.DB с диалектом и миграциями.
type DB struct {
	*sql.DB
	pg bool
}

// Open открывает (создавая при необходимости) базу и накатывает миграции.
// path — файл SQLite или DSN postgres://… (PG-бэкенд для 2+ реплик шлюза,
// карточка ha-control-plane).
func Open(path string) (*DB, error) {
	var d *DB
	if strings.HasPrefix(path, "postgres://") || strings.HasPrefix(path, "postgresql://") {
		db, err := sql.Open("pgx", path)
		if err != nil {
			return nil, fmt.Errorf("store: open pg %s: %w", safeDSN(path), err)
		}
		db.SetMaxOpenConns(10)
		d = &DB{DB: db, pg: true}
	} else {
		dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("store: open %s: %w", path, err)
		}
		// sqlite + database/sql: одно соединение достаточно и безопаснее для WAL-записи.
		db.SetMaxOpenConns(1)
		d = &DB{DB: db}
	}
	if err := d.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping %s: %w", safeDSN(path), err)
	}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return d, nil
}

// safeDSN — DSN без учётных данных для логов/ошибок.
func safeDSN(path string) string {
	if i := strings.Index(path, "@"); i > 0 && strings.Contains(path[:i], ":") {
		return path[:strings.Index(strings.TrimPrefix(path, "postgresql://"), ":")] + ":***@" + path[i+1:]
	}
	return path
}

// rebind — плейсхолдеры ? → $1..$n (pgx); sqlite остаётся с ?.
func (d *DB) rebind(query string) string {
	if !d.pg {
		return query
	}
	var b strings.Builder
	n := 0
	inStr := false
	for _, r := range query {
		switch {
		case r == '\'':
			inStr = !inStr
			b.WriteRune(r)
		case r == '?' && !inStr:
			n++
			b.WriteString("$" + strconv.Itoa(n))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Query/QueryRow/Exec — shadow-методы встроенных *sql.DB: rebind + (pg)
// конвертация диалектных выражений SQLite в эквиваленты PG. Новые запросы
// пишутся в sqlite-диалекте — конвертация здесь, единая точка (карточка
// ha-control-plane, план п.2).
var (
	sqliteHourBucketRe = regexp.MustCompile(`strftime\('%Y-%m-%dT%H:00:00Z', ([a-zA-Z_.]+)\)`)
	sqliteDayBucketRe  = regexp.MustCompile(`strftime\('%Y-%m-%dT00:00:00Z', ([a-zA-Z_.]+)\)`)
)

func (d *DB) convert(query string) string {
	if !d.pg {
		return query
	}
	// datetime('now') — метка времени в формате sqlite (TEXT-схема).
	q := strings.ReplaceAll(query, "datetime('now')", "to_char(now(), 'YYYY-MM-DD HH24:MI:SS')")
	// datetime('now', ?) с параметром-модификатором ("-N hours") — сдвиг от
	// текущего момента: pg делает интервальную арифметикой.
	q = strings.ReplaceAll(q, "datetime('now', ?)", "to_char(now() + ?::interval, 'YYYY-MM-DD HH24:MI:SS')")
	// date('now', ?) с параметром "-N days" и date('now') — границы
	// диапазонов в TEXT-домене (лексические сравнения как в sqlite).
	q = strings.ReplaceAll(q, "date('now', ?)", "to_char(current_date + ?::interval, 'YYYY-MM-DD HH24:MI:SS')")
	q = strings.ReplaceAll(q, "date('now')", "to_char(current_date, 'YYYY-MM-DD HH24:MI:SS')")
	// INSERT OR IGNORE не конвертируется автоматически: используйте
	// execIgnore (pg добавляет ON CONFLICT DO NOTHING в конец statement).
	// Произвольный "INSERT OR IGNORE" на pg упадёт громко, а не потеряет
	// семантику тихо.
	q = sqliteHourBucketRe.ReplaceAllString(q, `to_char($1::timestamp, 'YYYY-MM-DD"T"HH24:00:00"Z"')`)
	q = sqliteDayBucketRe.ReplaceAllString(q, `to_char($1::timestamp, 'YYYY-MM-DD"T"00:00:00"Z"')`)
	return q
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(d.rebind(d.convert(query)), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(d.rebind(d.convert(query)), args...)
}

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(d.rebind(d.convert(query)), args...)
}

// execIgnore — идемпотентный INSERT: sqlite "INSERT OR IGNORE", pg —
// ON CONFLICT DO NOTHING в конце statement'а (query — обычный INSERT INTO).
func (d *DB) execIgnore(query string, args ...any) (sql.Result, error) {
	if d.pg {
		query = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";")) + " ON CONFLICT DO NOTHING"
	} else {
		query = strings.Replace(query, "INSERT INTO", "INSERT OR IGNORE INTO", 1)
	}
	return d.DB.Exec(d.rebind(d.convert(query)), args...)
}

// insertID — ID свежей строки автоинкрементного ключа: sqlite —
// LastInsertId; pg — RETURNING id (дописывается к INSERT).
func (d *DB) insertID(query string, args ...any) (int64, error) {
	if d.pg {
		return d.insertIDPG(query, args...)
	}
	res, err := d.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) insertIDPG(query string, args ...any) (int64, error) {
	var id int64
	if err := d.DB.QueryRow(d.rebind(d.convert(query))+" RETURNING id", args...).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// migrate применяет встроенные миграции по порядку имён: каждая — в
// транзакции; версия схемы — PRAGMA user_version (sqlite) или таблица
// schema_migrations (pg); уже применённые пропускаются.
func (d *DB) migrate() error {
	version, err := d.schemaVersion()
	if err != nil {
		return err
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
		bodyStr := d.convertMigration(string(body))
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(d.rebind(bodyStr)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := d.setSchemaVersion(tx, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// schemaVersion — текущая версия схемы (sqlite: PRAGMA user_version; pg:
// schema_migrations.version единственной строки).
func (d *DB) schemaVersion() (int, error) {
	if !d.pg {
		var version int
		if err := d.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return 0, fmt.Errorf("read user_version: %w", err)
		}
		return version, nil
	}
	if _, err := d.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL)`); err != nil {
		return 0, fmt.Errorf("schema_migrations create: %w", err)
	}
	var version int
	err := d.DB.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	return version, nil
}

func (d *DB) setSchemaVersion(tx *sql.Tx, v int) error {
	if !d.pg {
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
			return fmt.Errorf("set user_version %d: %w", v, err)
		}
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES ($1)`, v); err != nil {
		return fmt.Errorf("set schema_migrations %d: %w", v, err)
	}
	return nil
}

// convertMigration — SQL тела миграции sqlite → pg (общая схема: TEXT-
// таймстампы; известные паттерны схемы — конечный список):
//   - INTEGER PRIMARY KEY AUTOINCREMENT → BIGSERIAL PRIMARY KEY
//   - DEFAULT (datetime('now')) → DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS')
func (d *DB) convertMigration(body string) string {
	if !d.pg {
		return body
	}
	b := strings.ReplaceAll(body,
		"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY")
	b = strings.ReplaceAll(b,
		"DEFAULT (datetime('now'))", "DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS')")
	b = strings.ReplaceAll(b,
		"DEFAULT datetime('now')", "DEFAULT to_char(now(), 'YYYY-MM-DD HH24:MI:SS')")
	return b
}

// now возвращает UTC-строку того же формата, что datetime('now') в sqlite.
func now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }
