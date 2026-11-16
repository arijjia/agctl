package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

var testUsage = Usage{
	Tagline: "тест",
	Sections: []UsageSection{
		{Title: "A", Rows: []UsageRow{{Name: "serve", Desc: []string{"поднять", "--home DIR"}}, {Name: "dev up [DIR]", Desc: []string{"рабочее место"}}}},
		{Title: "B", Rows: []UsageRow{{Name: "version", Desc: []string{"версия"}}}},
	},
	Env:    []UsageRow{{Name: "AGCTL_HOME", Desc: []string{"дом"}}},
	Footer: "секреты — только env",
}

// checkUsage — общая проверка справки бинаря: без ANSI в plain-рендере, все
// команды на месте, описания выровнены на одной колонке.
func checkUsage(t *testing.T, u Usage, names ...string) {
	t.Helper()
	var buf bytes.Buffer
	u.Render(&buf, false)
	out := buf.String()
	if strings.ContainsRune(out, '\x1b') {
		t.Fatal("plain-рендер содержит ANSI-коды")
	}
	for _, name := range names {
		if !strings.Contains(out, name) {
			t.Errorf("справка не содержит %q", name)
		}
	}
	col := -1
	for _, r := range u.CommandRows() {
		line := rowLine(out, r.Name)
		if line == "" {
			t.Fatalf("строка %q не найдена в справке", r.Name)
		}
		pos := strings.Index(line, r.Desc[0])
		if pos < 0 {
			t.Fatalf("описание %q не найдено в строке %q", r.Desc[0], r.Name)
		}
		if col == -1 {
			col = pos
		}
		if pos != col {
			t.Errorf("строка %q: описание на колонке %d, ждали %d (%q)", r.Name, pos, col, line)
		}
	}
}

func TestRenderUsagePlain(t *testing.T) {
	checkUsage(t, testUsage, "serve", "dev up", "version", "AGCTL_HOME", "секреты")
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
	testUsage.Render(&colorBuf, true)
	testUsage.Render(&plainBuf, false)
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
	if UseColor(f) {
		t.Fatal("NO_COLOR должен отключать цвета")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if UseColor(f) {
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
