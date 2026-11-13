package backlog

import "testing"

// validCard — минимально валидная карточка для подмены полей в тестах.
func validCard() Card {
	return Card{
		Schema: "1.0.0", ID: "fix-thing", Title: "Починить",
		Status: "open", Priority: "P2", Size: "S", Area: "meta",
		Repo: "arijjia/agctl", Created: "2026-10-12", Updated: "2026-10-12",
	}
}

func TestValidateSchemaParity(t *testing.T) {
	cases := []struct {
		schema string
		ok     bool
	}{
		{"1.0.0", true},
		{"1.1.0", true},  // MINOR gitctl (sessions:) — обратно совместимы
		{"1.5.0", true},  // будущие MINOR тоже терпимы (политика semver формата)
		{"2.0.0", false}, // чужой MAJOR — отказ
		{"0.x", false},
		{"", false},
		{"1.x", false},
		{"abc", false},
	}
	for _, tc := range cases {
		c := validCard()
		c.Schema = tc.schema
		f := File{Card: c, Path: "backlog/open/2026-10-12-fix-thing.md"}
		errs := f.Validate()
		if tc.ok && len(errs) != 0 {
			t.Errorf("schema %q: ожидали валидность, получили %v", tc.schema, errs)
		}
		if !tc.ok && len(errs) == 0 {
			t.Errorf("schema %q: ожидали ошибку", tc.schema)
		}
	}
}

// TestValidateLegacyDatedID — легаси-карточки с датой в id (созданы до
// фиксации «id == стем»): night-sweep-2026-10-12 в 2026-10-12-night-sweep.md
// валидны, пока хвост id равен created (parity с gitctl).
func TestValidateLegacyDatedID(t *testing.T) {
	c := validCard()
	c.ID = "night-sweep-2026-10-12"
	f := File{Card: c, Path: "backlog/done/2026-10-12-night-sweep.md"}
	if errs := f.Validate(); len(errs) != 0 {
		t.Fatalf("легаси датированный id должен быть валиден: %v", errs)
	}
	// Хвост не равен created — уже не легаси-паттерн, а расхождение.
	c.Created = "2026-10-13"
	f = File{Card: c, Path: "backlog/done/2026-10-12-night-sweep.md"}
	if errs := f.Validate(); len(errs) == 0 {
		t.Fatal("датированный id с чужой датой должен падать")
	}
	// Обычная карточка по-прежнему валидна.
	c = validCard()
	f = File{Card: c, Path: "backlog/open/2026-10-12-fix-thing.md"}
	if errs := f.Validate(); len(errs) != 0 {
		t.Fatalf("обычная карточка: %v", errs)
	}
	// Посторонний стем — ошибка, как и раньше.
	f = File{Card: c, Path: "backlog/open/2026-10-12-other-thing.md"}
	if errs := f.Validate(); len(errs) == 0 {
		t.Fatal("чужой стем должен падать")
	}
}
