package main

// usage.go — красивая справка agctl: секции, выравненная двухколонная
// таблица, ANSI-цвета при терминале. Контент таблицы и справка должны
// меняться синхронно с командами в main() и флагами подкоманд.

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// usageRow — строка таблицы справки: имя команды/переменной и описание
// (первый элемент — на строке имени, остальные — с отступом под ним).
type usageRow struct {
	name string
	desc []string
}

// usageSection — группа строк с заголовком.
type usageSection struct {
	title string
	rows  []usageRow
}

// usageSections — команды agctl по смыслу: управление платформой, работа
// разработчика, служебные.
var usageSections = []usageSection{
	{
		title: "Управление платформой",
		rows: []usageRow{
			{name: "serve", desc: []string{
				"поднять платформу: UI + API + личные runtime пользователей",
				"--home DIR · --addr H:P · --image T · --bin-dir D · --profile NAME · --llm-*",
			}},
			{name: "migrate", desc: []string{"накатить миграции SQLite вручную"}},
			{name: "seed-demo-users", desc: []string{"локальные тестовые роли user/developer/admin (после первого admin)"}},
		},
	},
	{
		title: "Работа разработчика",
		rows: []usageRow{
			{name: "connect", desc: []string{
				"SSH в персональный контейнер (роль developer)",
				"--port SSH_PORT · --host HOST · --user root · --identity KEY · --print",
			}},
			{name: "sync", desc: []string{
				"Mutagen-синхронизация локальной папки с /workspace контейнера",
				"sync create --name N --local DIR --port P · list|pause|resume|flush|terminate",
			}},
			{name: "completion fish", desc: []string{"скрипт автодополнения для fish"}},
			{name: "config", desc: []string{"показать эффективный ~/.config/agctl/config.yaml"}},
		},
	},
	{
		title: "Служебные",
		rows: []usageRow{
			{name: "backlog", desc: []string{"карточки задач: new|list|top|check|start|done|cancel|note|migrate"}},
			{name: "local connect", desc: []string{"experimental: коннектор локального backend к серверу"}},
			{name: "version", desc: []string{"версия и сборка"}},
			{name: "help", desc: []string{"эта справка"}},
		},
	},
}

// usageEnv — переменные окружения agctl.
var usageEnv = []usageRow{
	{name: "AGCTL_HOME", desc: []string{"дом данных agctl (по умолчанию ~/.agctl)"}},
	{name: "AGCTL_ADDR", desc: []string{"адрес UI/API (по умолчанию 127.0.0.1:8929)"}},
	{name: "AGCTL_PROFILE", desc: []string{"профиль платформы (по умолчанию из config.yaml, иначе agctl)"}},
	{name: "AGCTL_CONFIG", desc: []string{"путь к config.yaml (по умолчанию ~/.config/agctl/config.yaml)"}},
}

// usage печатает справку в stderr: цвета — только если поток терминальный.
func usage() {
	renderUsage(os.Stderr, useColor(os.Stderr))
}

// renderUsage рендерит справку: заголовок с версией, секции команд и
// переменных окружения выровненной таблицей. color включает ANSI-коды
// (bold-заголовки, cyan-имена, dim-служебные строки).
func renderUsage(w io.Writer, color bool) {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	nameCol := 0
	for _, rows := range [][]usageRow{envRows(), sectionRows()} {
		for _, r := range rows {
			if len(r.name) > nameCol {
				nameCol = len(r.name)
			}
		}
	}
	nameCol += 2

	title := "agctl — платформа агентских рабочих мест (один бинарь)"
	if version != "" && version != "dev" {
		title = "agctl " + version + " — платформа агентских рабочих мест"
	}
	fmt.Fprintln(w, paint("1", title))
	fmt.Fprintln(w, paint("2", "Использование: agctl <команда> [флаги]"))

	for _, sec := range usageSections {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint("1", sec.title))
		for _, r := range sec.rows {
			writeRow(w, r, nameCol, paint)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, paint("1", "Переменные окружения"))
	for _, r := range usageEnv {
		writeRow(w, r, nameCol, paint)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, paint("2", "Секреты — только через env (например AGCTL_LLM_API_KEY), в config.yaml их не пишут."))
}

// writeRow печатает строку таблицы: имя в cyan, описание — дефолтным цветом,
// продолжения описания — dim с выравниванием по колонке имени.
func writeRow(w io.Writer, r usageRow, nameCol int, paint func(code, s string) string) {
	pad := strings.Repeat(" ", nameCol-len(r.name))
	fmt.Fprintln(w, "  "+paint("36", r.name)+pad+r.desc[0])
	indent := strings.Repeat(" ", 2+nameCol)
	for _, extra := range r.desc[1:] {
		fmt.Fprintln(w, indent+paint("2", extra))
	}
}

// sectionRows — все имена команд (для расчёта ширины колонки).
func sectionRows() []usageRow {
	var rows []usageRow
	for _, sec := range usageSections {
		rows = append(rows, sec.rows...)
	}
	return rows
}

// envRows — имена переменных окружения (для расчёта ширины колонки).
func envRows() []usageRow { return usageEnv }

// useColor — включать ли ANSI: уважаем NO_COLOR и TERM=dumb, цвета только
// когда поток — символьное устройство (терминал), не пайп/файл.
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
