package cli

// usage.go — красивая справка бинарей agctl: секции, выравненная
// двухколонная таблица, ANSI-цвета при терминале. Содержимое (команды,
// переменные окружения) задаёт каждая точка входа; таблица должна меняться
// синхронно с её диспетчером команд.

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// UsageRow — строка таблицы справки: имя команды/переменной и описание
// (первый элемент — на строке имени, остальные — с отступом под ним).
type UsageRow struct {
	Name string
	Desc []string
}

// UsageSection — группа строк с заголовком.
type UsageSection struct {
	Title string
	Rows  []UsageRow
}

// Usage — справка одного бинаря.
type Usage struct {
	Tagline  string // «платформа агентских рабочих мест: …»
	Sections []UsageSection
	Env      []UsageRow
	Footer   string // приглушённая строка в конце; пусто — нет
}

// Print печатает справку в stderr (подсказка при ошибке): цвета — только если
// поток терминальный.
func (u Usage) Print() { u.Render(os.Stderr, UseColor(os.Stderr)) }

// PrintHelp — справка по запросу (help, -h, --help): в stdout, как принято
// для явной справки (cli-std: --help — exit 0 и метки в выводе).
func (u Usage) PrintHelp() { u.Render(os.Stdout, UseColor(os.Stdout)) }

// Render рендерит справку: заголовок с версией, секции команд и
// переменных окружения выровненной таблицей. color включает ANSI-коды
// (bold-заголовки, cyan-имена, dim-служебные строки).
func (u Usage) Render(w io.Writer, color bool) {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	nameCol := 0
	for _, r := range append(u.Env, u.CommandRows()...) {
		if len(r.Name) > nameCol {
			nameCol = len(r.Name)
		}
	}
	nameCol += 2

	title := binName + " — " + u.Tagline
	if version != "" && version != "dev" {
		title = binName + " " + version + " — " + u.Tagline
	}
	fmt.Fprintln(w, paint("1", title))
	fmt.Fprintln(w, paint("2", "Использование: "+binName+" <команда> [флаги]"))

	for _, sec := range u.Sections {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint("1", sec.Title))
		for _, r := range sec.Rows {
			writeRow(w, r, nameCol, paint)
		}
	}
	if len(u.Env) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint("1", "Переменные окружения"))
		for _, r := range u.Env {
			writeRow(w, r, nameCol, paint)
		}
	}
	if u.Footer != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint("2", u.Footer))
	}
}

// CommandRows — все строки команд (ширина колонки, тесты).
func (u Usage) CommandRows() []UsageRow {
	var rows []UsageRow
	for _, sec := range u.Sections {
		rows = append(rows, sec.Rows...)
	}
	return rows
}

// writeRow печатает строку таблицы: имя в cyan, описание — дефолтным цветом,
// продолжения описания — dim с выравниванием по колонке имени.
func writeRow(w io.Writer, r UsageRow, nameCol int, paint func(code, s string) string) {
	pad := strings.Repeat(" ", nameCol-len(r.Name))
	fmt.Fprintln(w, "  "+paint("36", r.Name)+pad+r.Desc[0])
	indent := strings.Repeat(" ", 2+nameCol)
	for _, extra := range r.Desc[1:] {
		fmt.Fprintln(w, indent+paint("2", extra))
	}
}

// UseColor — включать ли ANSI: уважаем NO_COLOR и TERM=dumb, цвета только
// когда поток — символьное устройство (терминал), не пайп/файл.
func UseColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
