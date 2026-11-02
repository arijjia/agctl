package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestRenderUsagePlain — без цветов: ни одного ANSI-кода, колонка описаний
// выровнена (все строки секций с одним смещением), все команды на месте.
func TestRenderUsagePlain(t *testing.T) {
	var buf bytes.Buffer
	renderUsage(&buf, false)
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') {
		t.Fatal("plain-рендер содержит ANSI-коды")
	}
	for _, name := range []string{
		"serve", "migrate", "seed-demo-users", "backlog", "connect",
		"local connect", "sync", "completion fish", "config", "version", "help",
		"AGCTL_HOME", "AGCTL_CONFIG",
	} {
		if !strings.Contains(out, name) {
			t.Errorf("справка не содержит %q", name)
		}
	}
	// выравнивание: у всех строк команд описание начинается на одной колонке
	col := -1
	for _, sec := range usageSections {
		for _, r := range sec.rows {
			line := rowLine(out, r.name)
			if line == "" {
				t.Fatalf("строка %q не найдена в справке", r.name)
			}
			pos := strings.Index(line, r.desc[0])
			if pos < 0 {
				t.Fatalf("описание %q не найдено в строке %q", r.desc[0], r.name)
			}
			if col == -1 {
				col = pos
			}
			if pos != col {
				t.Errorf("строка %q: описание на колонке %d, ждали %d (%q)", r.name, pos, col, line)
			}
		}
	}
}

// rowLine возвращает первую строку справки, чей контент (после отступа)
// начинается с name.
func rowLine(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), name+" ") {
			return line
		}
	}
	return ""
}

// TestRenderUsageColor — с цветами есть ANSI-коды, а после их удаления
// содержимое совпадает с plain-вариантом.
func TestRenderUsageColor(t *testing.T) {
	var colorBuf, plainBuf bytes.Buffer
	renderUsage(&colorBuf, true)
	renderUsage(&plainBuf, false)
	if !strings.Contains(colorBuf.String(), "\x1b[1m") || !strings.Contains(colorBuf.String(), "\x1b[36m") {
		t.Fatal("цветной рендер не содержит ожидаемых ANSI-кодов")
	}
	if stripANSI(colorBuf.String()) != plainBuf.String() {
		t.Fatal("цветной и plain-рендеры различаются текстом")
	}
}

// TestUseColorEnv — NO_COLOR и TERM=dumb отключают цвета.
func TestUseColorEnv(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("нет %s для проверки: %v", os.DevNull, err)
	}
	defer f.Close()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "")
	if useColor(f) {
		t.Fatal("NO_COLOR должен отключать цвета")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if useColor(f) {
		t.Fatal("TERM=dumb должен отключать цвета")
	}
	t.Setenv("TERM", "")
}

// stripANSI убирает SGR-последовательности из строки.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
