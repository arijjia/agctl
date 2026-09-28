package backlog

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Main — точка входа CLI: agctl backlog <команда> [флаги].
// Возвращает код выхода.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	fs := flag.NewFlagSet("backlog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "корень бэклога (по умолчанию — поиск backlog/ от cwd)")
	repo := fs.String("repo", "", "имя репозитория для шапки карточек")
	_ = fs.Parse(args)
	cmd := fs.Arg(0)
	rest := fs.Args()[1:]

	r, err := resolveRoot(*root)
	if err != nil {
		if cmd == "new" || cmd == "init" {
			// new может создать backlog/ в cwd, если его нет
			r = filepath.Join(mustAbs(), "backlog")
		} else {
			fmt.Fprintln(stderr, "ошибка:", err)
			return 1
		}
	}

	switch cmd {
	case "new":
		return cmdNew(r, *repo, rest, stdout, stderr)
	case "list":
		return cmdList(r, rest, stdout, stderr)
	case "top":
		return cmdTop(r, rest, stdout, stderr)
	case "check":
		return cmdCheck(r, stdout, stderr)
	case "start", "done", "cancel":
		return cmdStatus(r, cmd, rest, stdout, stderr)
	case "note":
		return cmdNote(r, rest, stdout, stderr)
	case "migrate":
		return cmdMigrate(r, *repo, stdout, stderr)
	case "help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "неизвестная команда %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `agctl backlog — ведение бэклога (формат v1, schema 1.0.0)

Команды:
  new --title "..." [--priority P2] [--size M] [--area meta] [--tags a,b] [--desc "..."]
  list [--all]                 открытые карточки по приоритету
  top [N]                      топ N задач (по умолчанию 3)
  check                        валидация всех карточек
  start <id> | done <id> | cancel <id>
  note <id> <текст>            дописать строку в Журнал карточки
  migrate [--repo имя]         привести шапки к schema 1.0.0

Флаги: --root <dir> — корень бэклога (иначе поиск backlog/ от cwd)
`)
}

func resolveRoot(flagRoot string) (string, error) {
	if flagRoot != "" {
		return flagRoot, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return Root(cwd)
}

func mustAbs() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func today() string { return time.Now().Format("2006-01-02") }

// ── new ──────────────────────────────────────────────────────────────────────

func cmdNew(root, repo string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "название задачи (обязательно)")
	priority := fs.String("priority", "P2", "P1|P2|P3")
	size := fs.String("size", "M", "S|M|L")
	area := fs.String("area", "meta", "домен задачи")
	tags := fs.String("tags", "", "теги через запятую")
	desc := fs.String("desc", "", "текст в «Описание»")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *title == "" {
		fmt.Fprintln(stderr, "ошибка: нужен --title")
		return 2
	}
	c := Card{
		Schema: Schema, ID: Slug(*title), Title: *title,
		Status: "open", Priority: *priority, Size: *size, Area: *area,
		Repo: repo, Created: today(), Updated: today(),
	}
	if t := splitCSV(*tags); len(t) > 0 {
		c.Tags = t
	}
	body := "\n## Описание\n\n" + *desc + "\n\n## Критерии готовности\n\n- \n\n## Журнал\n\n- " + today() + ": занесено\n"
	f := File{Card: c, Body: body}
	openDir := filepath.Join(root, "open")
	if err := os.MkdirAll(openDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	path := filepath.Join(openDir, today()+"-"+c.ID+".md")
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintln(stderr, "ошибка: карточка с таким id уже существует:", path)
		return 1
	}
	content, err := f.Render()
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ── list / top ───────────────────────────────────────────────────────────────

func cmdList(root string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "включая done/cancelled")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	files, err := LoadAll(root)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	for _, f := range files {
		if !*all && f.Card.Status != "open" && f.Card.Status != "in-progress" {
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n",
			f.Card.Priority, f.Card.Status, f.Card.ID, f.Card.Size, f.Card.Title)
	}
	return 0
}

func cmdTop(root string, args []string, stdout, stderr io.Writer) int {
	n := 3
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &n)
	}
	files, err := LoadAll(root)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	shown := 0
	for _, f := range files {
		if shown >= n {
			break
		}
		if f.Card.Status != "open" && f.Card.Status != "in-progress" {
			continue
		}
		fmt.Fprintf(stdout, "%s [%s] %s\n", f.Card.Priority, f.Card.Status, f.Card.Title)
		shown++
	}
	return 0
}

// ── check ────────────────────────────────────────────────────────────────────

func cmdCheck(root string, stdout, stderr io.Writer) int {
	files, err := LoadAll(root)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	bad := 0
	seen := map[string]string{}
	for _, f := range files {
		for _, e := range f.Validate() {
			fmt.Fprintln(stderr, e)
			bad++
		}
		if prev, ok := seen[f.Card.ID]; ok && f.Card.Status == "open" {
			fmt.Fprintf(stderr, "дубликат id %q: %s и %s\n", f.Card.ID, prev, f.Path)
			bad++
		}
		seen[f.Card.ID] = f.Path
	}
	if bad > 0 {
		fmt.Fprintf(stdout, "FAIL: проблем: %d\n", bad)
		return 1
	}
	fmt.Fprintf(stdout, "OK: карточек: %d\n", len(files))
	return 0
}

// ── start / done / cancel ────────────────────────────────────────────────────

func cmdStatus(root, cmd string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintf(stderr, "ошибка: укажите id карточки\n")
		return 2
	}
	id := args[0]
	f, path, err := findOpen(root, id)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	var status, line string
	switch cmd {
	case "start":
		status, line = "in-progress", today()+": start — в работе (agctl)"
	case "done":
		status, line = "done", today()+": done — завершено (agctl)"
	case "cancel":
		status, line = "cancelled", today()+": cancel — отменено (agctl)"
	}
	f.Card.Status = status
	f.Card.Updated = today()
	f.Body = AppendJournal(f.Body, line)
	if err := save(f, path, root, status); err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

// findOpen ищет карточку по id среди open (и in-progress).
func findOpen(root, id string) (File, string, error) {
	openDir := filepath.Join(root, "open")
	entries, err := os.ReadDir(openDir)
	if os.IsNotExist(err) {
		return File{}, "", fmt.Errorf("backlog/open не найден в %s", root)
	}
	if err != nil {
		return File{}, "", err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || !strings.Contains(name, id) {
			continue
		}
		p := filepath.Join(openDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			return File{}, "", err
		}
		f, err := Parse(p, string(b))
		if err != nil {
			return File{}, "", err
		}
		if f.Card.ID == id {
			return f, p, nil
		}
	}
	return File{}, "", fmt.Errorf("карточка %q не найдена в open/", id)
}

// save перезаписывает карточку; done/cancelled переезжают в done/.
func save(f File, path, root, status string) error {
	content, err := f.Render()
	if err != nil {
		return err
	}
	if status == "done" || status == "cancelled" {
		doneDir := filepath.Join(root, "done")
		if err := os.MkdirAll(doneDir, 0o755); err != nil {
			return err
		}
		newPath := filepath.Join(doneDir, filepath.Base(path))
		if err := os.WriteFile(newPath, []byte(content), 0o644); err != nil {
			return err
		}
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ── note ─────────────────────────────────────────────────────────────────────

func cmdNote(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "ошибка: usage: agctl backlog note <id> <текст>")
		return 2
	}
	id, text := args[0], strings.Join(args[1:], " ")
	f, path, err := findOpen(root, id)
	if err != nil {
		// заметка в закрытую карточку — тоже валидно, ищем в done/
		f2, p2, e2 := findAny(root, id)
		if e2 != nil {
			fmt.Fprintln(stderr, "ошибка:", err)
			return 1
		}
		f, path = f2, p2
	}
	f.Card.Updated = today()
	f.Body = AppendJournal(f.Body, today()+": "+text)
	content, err := f.Render()
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

func findAny(root, id string) (File, string, error) {
	for _, sub := range []string{"open", "done"} {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			f, err := Parse(p, string(b))
			if err == nil && f.Card.ID == id {
				return f, p, nil
			}
		}
	}
	return File{}, "", fmt.Errorf("карточка %q не найдена", id)
}

// ── migrate ──────────────────────────────────────────────────────────────────

func cmdMigrate(root, repo string, stdout, stderr io.Writer) int {
	files, err := LoadAll(root)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	fixed := 0
	for _, f := range files {
		c := &f.Card
		changed := false
		if c.Schema == "" || c.Schema == "0.x" {
			c.Schema, changed = Schema, true
		}
		if c.Priority == "" {
			c.Priority, changed = "P2", true
		}
		if c.Size == "" {
			c.Size, changed = "M", true
		}
		if c.Area == "" {
			c.Area, changed = "meta", true
		}
		if c.Repo == "" && repo != "" {
			c.Repo, changed = repo, true
		}
		if c.Updated == "" {
			c.Updated, changed = c.Created, true
		}
		if !changed {
			continue
		}
		content, err := f.Render()
		if err != nil {
			fmt.Fprintln(stderr, "ошибка:", err)
			return 1
		}
		if err := os.WriteFile(f.Path, []byte(content), 0o644); err != nil {
			fmt.Fprintln(stderr, "ошибка:", err)
			return 1
		}
		fixed++
	}
	fmt.Fprintf(stdout, "migrate: обновлено карточек: %d\n", fixed)
	return 0
}
