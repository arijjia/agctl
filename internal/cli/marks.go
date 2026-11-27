package cli

// marks.go — метки инвазивности команд (cli-std): каждая исполняемая листовая
// подкоманда несёт метку в описании — и в справке, и в fish-комплишенах.
// Группа получает метку, только если все её листья единогласны (агрегация);
// смешанные группы и флаги — без метки. Канон меток дерева команд держит тест
// бинаря (CommandMarks), он же сверяет с ним справку и комплишены.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Метки инвазивности.
const (
	MarkRead   = "🔍"  // читающая
	MarkWrite  = "✏️" // мутирующая: меняет данные или настройки
	MarkEffect = "⚠️" // с побочным эффектом: данных не меняет, но меняет мир — демон, контейнер, браузер, сеть
)

// MarksLegend — строка-легенда для справки.
const MarksLegend = "Метки: 🔍 читающая · ✏️ мутирующая · ⚠️ с побочным эффектом (демон, контейнер, браузер, сеть)."

// CommandMarks — канон меток дерева команд: путь листа ("dev certs list") →
// метка.
type CommandMarks map[string]string

// Mark — метка команды по пути: у листа — своя, у группы — общая для всех
// листьев под ней, если она одна, иначе "". ok=false — такой команды нет.
func (m CommandMarks) Mark(path string) (mark string, ok bool) {
	if v, leaf := m[path]; leaf {
		return v, true
	}
	prefix := path + " "
	for p, v := range m {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if !ok {
			mark, ok = v, true
			continue
		}
		if v != mark {
			mark = ""
		}
	}
	return mark, ok
}

// Leaves — пути листьев по алфавиту.
func (m CommandMarks) Leaves() []string {
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// HasMark — описание начинается с метки mark (и пробела); для mark "" — ни с
// одной из меток.
func HasMark(desc, mark string) bool {
	desc = strings.TrimSpace(desc)
	if mark == "" {
		for _, m := range []string{MarkRead, MarkWrite, MarkEffect} {
			if strings.HasPrefix(desc, m) {
				return false
			}
		}
		return true
	}
	return strings.HasPrefix(desc, mark+" ")
}

var (
	fishCondRe = regexp.MustCompile(`-n '([^']*)'`)
	fishArgRe  = regexp.MustCompile(` -a (\S+)`)
	fishDescRe = regexp.MustCompile(` -d '([^']*)'`)
)

// CheckFishMarks сверяет fish-комплишены с каноном: каждая строка, которая
// предлагает подкоманду (-a <имя> с условием из parents: условие -n → путь
// родителя, "" — верхний уровень), несёт метку этой команды. Возвращает
// найденные пути и расхождения.
func CheckFishMarks(script string, marks CommandMarks, parents map[string]string) (seen map[string]bool, problems []string) {
	seen = map[string]bool{}
	for _, line := range strings.Split(script, "\n") {
		if !strings.HasPrefix(line, "complete ") || strings.Contains(line, " -l ") || strings.Contains(line, " -s ") {
			continue
		}
		cm, am := fishCondRe.FindStringSubmatch(line), fishArgRe.FindStringSubmatch(line)
		if cm == nil || am == nil {
			continue
		}
		parent, known := parents[cm[1]]
		if !known {
			continue
		}
		name := strings.Trim(am[1], `'"`)
		if strings.ContainsAny(name, " ($") {
			problems = append(problems, "несколько подкоманд одной строкой — нужна строка с описанием на каждую: "+line)
			continue
		}
		path := strings.TrimSpace(parent + " " + name)
		mark, ok := marks.Mark(path)
		if !ok {
			problems = append(problems, "нет в каноне меток: "+path)
			continue
		}
		seen[path] = true
		dm := fishDescRe.FindStringSubmatch(line)
		if dm == nil {
			problems = append(problems, "без описания: "+path)
			continue
		}
		if !HasMark(dm[1], mark) {
			want := mark
			if want == "" {
				want = "без метки (смешанная группа)"
			}
			problems = append(problems, path+": ждали «"+want+"», в комплишене: "+dm[1])
		}
	}
	return seen, problems
}

var markPairRe = regexp.MustCompile(`(🔍|✏️|⚠️) ([a-z][a-z0-9-]*)`)

// children — прямые подкоманды prefix в каноне.
func (m CommandMarks) children(prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for p := range m {
		rest, ok := strings.CutPrefix(p, prefix+" ")
		if prefix == "" {
			rest, ok = p, true
		}
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// firstMark — первая метка в тексте ("" — нет ни одной).
func firstMark(s string) string {
	best, at := "", -1
	for _, m := range []string{MarkRead, MarkWrite, MarkEffect} {
		if i := strings.Index(s, m); i >= 0 && (at < 0 || i < at) {
			best, at = m, i
		}
	}
	return best
}

// CheckUsageMarks сверяет справку с каноном для подкоманд prefix: строка
// подкоманды — «  имя …» (с продолжениями ниже, отступ больше двух пробелов).
// У листа и единогласной группы первая метка в блоке — её метка; у смешанной
// группы в блоке у каждого листа стоит «метка имя». aliases — подкоманды,
// которым своя строка не нужна (синонимы).
func CheckUsageMarks(text, prefix string, marks CommandMarks, aliases ...string) []string {
	blocks := map[string]string{}
	want := map[string]bool{}
	for _, c := range marks.children(prefix) {
		want[c] = true
	}
	cur := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
			if want[name] {
				cur = name
				blocks[cur] += line + "\n"
				continue
			}
		}
		if cur != "" && strings.HasPrefix(line, "    ") {
			blocks[cur] += line + "\n"
			continue
		}
		cur = ""
	}
	skip := map[string]bool{}
	for _, a := range aliases {
		skip[a] = true
	}
	var problems []string
	for _, c := range marks.children(prefix) {
		path := strings.TrimSpace(prefix + " " + c)
		block, ok := blocks[c]
		if !ok {
			if !skip[c] {
				problems = append(problems, "в справке нет строки "+path)
			}
			continue
		}
		mark, _ := marks.Mark(path)
		if mark != "" {
			if got := firstMark(block); got != mark {
				problems = append(problems, fmt.Sprintf("%s: ждали %s, в справке: %s", path, mark, strings.TrimSpace(block)))
			}
			continue
		}
		for _, leaf := range marks.children(path) {
			lm, _ := marks.Mark(path + " " + leaf)
			if !strings.Contains(block, lm+" "+leaf) {
				problems = append(problems, fmt.Sprintf("%s %s: ждали «%s %s» в строке группы: %s", path, leaf, lm, leaf, strings.TrimSpace(block)))
			}
		}
	}
	return problems
}

// CheckMarkPairs — каждая пара «метка имя» в тексте (сводные строки справки)
// совпадает с каноном для prefix имя.
func CheckMarkPairs(text, prefix string, marks CommandMarks) []string {
	var problems []string
	for _, m := range markPairRe.FindAllStringSubmatch(text, -1) {
		path := strings.TrimSpace(prefix + " " + m[2])
		if canon, ok := marks.Mark(path); ok && canon != m[1] {
			problems = append(problems, fmt.Sprintf("%s: в справке %s, по канону %s", path, m[1], canon))
		}
	}
	return problems
}
