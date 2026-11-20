package store

import (
	"errors"
	"strings"
)

// ErrConflict — запись с таким ключом уже есть.
var ErrConflict = errors.New("store: conflict")

// ErrStaleRevision — изменение отправлено с устаревшей ревизией: запись уже
// поменяли (другая вкладка лаунчера или таб dsh).
var ErrStaleRevision = errors.New("store: stale revision")

// Place — рабочее место поверх слота пользователя (Slot > 0). Mounts — JSON
// списка папок хоста (формат задаёт сервер). Образ — в settings слота.
type Place struct {
	UserID    int64
	ID        string
	Slot      int
	Name      string
	Mounts    string
	Revision  int64
	CreatedAt string
	UpdatedAt string
}

const placeCols = `user_id, id, slot, name, mounts, revision, created_at, updated_at`

func scanPlace(sc interface{ Scan(...any) error }) (Place, error) {
	var p Place
	err := sc.Scan(&p.UserID, &p.ID, &p.Slot, &p.Name, &p.Mounts, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// PlacesByUser — рабочие места пользователя по времени создания.
func (d *DB) PlacesByUser(userID int64) ([]Place, error) {
	rows, err := d.Query(`SELECT `+placeCols+` FROM places WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, wrap("places list", err)
	}
	defer rows.Close()
	var out []Place
	for rows.Next() {
		p, err := scanPlace(rows)
		if err != nil {
			return nil, wrap("places scan", err)
		}
		out = append(out, p)
	}
	return out, wrap("places rows", rows.Err())
}

// PlaceGet — одно рабочее место; нет — ErrNotFound.
func (d *DB) PlaceGet(userID int64, id string) (Place, error) {
	p, err := scanPlace(d.QueryRow(`SELECT `+placeCols+` FROM places WHERE user_id = ? AND id = ?`, userID, id))
	return p, wrap("place get", err)
}

// PlaceBySlot — рабочее место слота; нет — ErrNotFound.
func (d *DB) PlaceBySlot(userID int64, slot int) (Place, error) {
	p, err := scanPlace(d.QueryRow(`SELECT `+placeCols+` FROM places WHERE user_id = ? AND slot = ?`, userID, slot))
	return p, wrap("place by slot", err)
}

// PlaceCreate — новое рабочее место с ревизией 1; занятый id или слот —
// ErrConflict.
func (d *DB) PlaceCreate(p Place) (Place, error) {
	ts := now()
	_, err := d.Exec(`INSERT INTO places(`+placeCols+`) VALUES(?,?,?,?,?,1,?,?)`,
		p.UserID, p.ID, p.Slot, p.Name, p.Mounts, ts, ts)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return Place{}, ErrConflict
		}
		return Place{}, wrap("place create", err)
	}
	return d.PlaceGet(p.UserID, p.ID)
}

// PlaceUpdate — изменить имя и папки, если текущая ревизия равна
// expect; иначе ErrStaleRevision (нет записи — ErrNotFound).
func (d *DB) PlaceUpdate(p Place, expect int64) (Place, error) {
	res, err := d.Exec(`UPDATE places SET name = ?, mounts = ?, revision = revision + 1, updated_at = ?
		WHERE user_id = ? AND id = ? AND revision = ?`, p.Name, p.Mounts, now(), p.UserID, p.ID, expect)
	if err != nil {
		return Place{}, wrap("place update", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := d.PlaceGet(p.UserID, p.ID); err != nil {
			return Place{}, err
		}
		return Place{}, ErrStaleRevision
	}
	return d.PlaceGet(p.UserID, p.ID)
}

// PlaceDelete — удалить запись рабочего места (данные dsh-home не трогает).
func (d *DB) PlaceDelete(userID int64, id string) error {
	res, err := d.Exec(`DELETE FROM places WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return wrap("place delete", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
