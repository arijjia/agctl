// Package localbridge — мост к хосту локального режима (agctl dev): агент из
// контейнера вызывает на машине пользователя только команды из белого
// списка. Команда — argv-шаблон без shell: аргументы подставляются целиком в
// элементы argv и проверяются регулярками, так что инъекция в командную
// строку невозможна. Опасные инструменты (confirm) ждут подтверждения
// человека на хосте (`agctl dev bridge approve`). Каждый вызов пишется в
// журнал (без stdout/stderr — там могут быть данные проекта).
package localbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Arg — описание аргумента инструмента.
type Arg struct {
	Description string `yaml:"description" json:"description,omitempty"`
	// Pattern — регулярка значения (якорится целиком). Пусто = безопасный
	// дефолт: без управляющих символов и без ведущего «-» (защита от
	// подстановки опций).
	Pattern  string `yaml:"pattern" json:"pattern,omitempty"`
	Required bool   `yaml:"required" json:"required,omitempty"`
	Default  string `yaml:"default" json:"default,omitempty"`
}

// Tool — инструмент моста: argv-шаблон с плейсхолдерами {{имя}}.
type Tool struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Command     []string       `yaml:"command" json:"-"`
	Args        map[string]Arg `yaml:"args" json:"args,omitempty"`
	Dir         string         `yaml:"dir" json:"-"`
	TimeoutSec  int            `yaml:"timeout_sec" json:"timeoutSec,omitempty"`
	// Confirm — вызов ждёт подтверждения человека на хосте.
	Confirm bool `yaml:"confirm" json:"confirm,omitempty"`
}

var (
	toolNameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	placeholderRe  = regexp.MustCompile(`\{\{([a-z][a-z0-9_]*)\}\}`)
	defaultArgRe   = regexp.MustCompile(`^[^-\x00-\x1f][^\x00-\x1f]*$`)
	errUnknownTool = errors.New("инструмент не найден в белом списке моста")
)

// maxArgLen — потолок длины значения аргумента.
const maxArgLen = 1024

// DefaultTimeout — таймаут вызова по умолчанию.
const DefaultTimeout = 2 * time.Minute

// MaxOutput — потолок stdout/stderr, возвращаемых агенту (каждый поток).
const MaxOutput = 64 << 10

// Validate проверяет набор инструментов: имена, argv, плейсхолдеры и
// регулярки. Ошибка — с именем инструмента.
func Validate(tools []Tool) error {
	seen := map[string]bool{}
	for _, t := range tools {
		if !toolNameRe.MatchString(t.Name) {
			return fmt.Errorf("мост: имя инструмента %q — только [a-z0-9_], с буквы", t.Name)
		}
		if seen[t.Name] {
			return fmt.Errorf("мост: инструмент %q объявлен дважды", t.Name)
		}
		seen[t.Name] = true
		if len(t.Command) == 0 || strings.TrimSpace(t.Command[0]) == "" {
			return fmt.Errorf("мост: %s: пустая command", t.Name)
		}
		if placeholderRe.MatchString(t.Command[0]) {
			return fmt.Errorf("мост: %s: исполняемый файл не может быть плейсхолдером", t.Name)
		}
		for _, el := range t.Command {
			for _, m := range placeholderRe.FindAllStringSubmatch(el, -1) {
				if _, ok := t.Args[m[1]]; !ok {
					return fmt.Errorf("мост: %s: плейсхолдер {{%s}} не описан в args", t.Name, m[1])
				}
			}
		}
		for name, a := range t.Args {
			if !toolNameRe.MatchString(name) {
				return fmt.Errorf("мост: %s: имя аргумента %q — только [a-z0-9_]", t.Name, name)
			}
			if a.Pattern != "" {
				if _, err := regexp.Compile("^(?:" + a.Pattern + ")$"); err != nil {
					return fmt.Errorf("мост: %s.%s: pattern: %w", t.Name, name, err)
				}
			}
		}
	}
	return nil
}

// Expand собирает argv из шаблона и значений аргументов (с проверкой).
func (t Tool) Expand(values map[string]string) ([]string, error) {
	resolved := map[string]string{}
	for name, a := range t.Args {
		v, ok := values[name]
		if !ok || v == "" {
			v = a.Default
		}
		if v == "" {
			if a.Required {
				return nil, fmt.Errorf("аргумент %q обязателен", name)
			}
			resolved[name] = ""
			continue
		}
		if len(v) > maxArgLen {
			return nil, fmt.Errorf("аргумент %q длиннее %d байт", name, maxArgLen)
		}
		re := defaultArgRe
		if a.Pattern != "" {
			re = regexp.MustCompile("^(?:" + a.Pattern + ")$")
		}
		if !re.MatchString(v) {
			return nil, fmt.Errorf("аргумент %q не проходит проверку", name)
		}
		resolved[name] = v
	}
	for name := range values {
		if _, ok := t.Args[name]; !ok {
			return nil, fmt.Errorf("неизвестный аргумент %q", name)
		}
	}
	argv := make([]string, 0, len(t.Command))
	for _, el := range t.Command {
		// Элемент, целиком равный плейсхолдеру пустого необязательного
		// аргумента, выпадает из argv (а не превращается в "").
		if m := placeholderRe.FindStringSubmatch(el); m != nil && m[0] == el && resolved[m[1]] == "" {
			continue
		}
		argv = append(argv, placeholderRe.ReplaceAllStringFunc(el, func(ph string) string {
			return resolved[placeholderRe.FindStringSubmatch(ph)[1]]
		}))
	}
	return argv, nil
}

// Result — итог вызова для агента.
type Result struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated,omitempty"`
	DurationMS int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// Bridge — исполнитель белого списка с подтверждениями и журналом.
type Bridge struct {
	tools   map[string]Tool
	journal string // путь журнала (JSON lines); пусто = без журнала

	mu      sync.Mutex
	pending map[string]*Pending
	jmu     sync.Mutex
}

// Pending — вызов, ждущий подтверждения человека.
type Pending struct {
	ID      string            `json:"id"`
	Tool    string            `json:"tool"`
	Args    map[string]string `json:"args"`
	Argv    []string          `json:"argv"`
	Created time.Time         `json:"created"`
	verdict chan bool
}

// New создаёт мост; tools должны пройти Validate.
func New(tools []Tool, journal string) (*Bridge, error) {
	if err := Validate(tools); err != nil {
		return nil, err
	}
	b := &Bridge{tools: map[string]Tool{}, journal: journal, pending: map[string]*Pending{}}
	for _, t := range tools {
		b.tools[t.Name] = t
	}
	return b, nil
}

// Tools — описание инструментов для агента (без argv), по имени.
func (b *Bridge) Tools() []Tool {
	out := make([]Tool, 0, len(b.tools))
	for _, t := range b.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ApprovalTimeout — сколько вызов ждёт подтверждения.
var ApprovalTimeout = 5 * time.Minute

// Call выполняет инструмент. Для confirm-инструментов сначала ждёт решения
// человека (Approve/Deny) не дольше ApprovalTimeout.
func (b *Bridge) Call(ctx context.Context, name string, args map[string]string) (Result, error) {
	t, ok := b.tools[name]
	if !ok {
		return Result{}, errUnknownTool
	}
	argv, err := t.Expand(args)
	if err != nil {
		return Result{}, err
	}
	if t.Confirm {
		p := &Pending{ID: newID(), Tool: name, Args: args, Argv: argv, Created: time.Now(), verdict: make(chan bool, 1)}
		b.mu.Lock()
		b.pending[p.ID] = p
		b.mu.Unlock()
		b.log(map[string]any{"event": "pending", "id": p.ID, "tool": name, "argv": argv})
		defer func() {
			b.mu.Lock()
			delete(b.pending, p.ID)
			b.mu.Unlock()
		}()
		select {
		case ok := <-p.verdict:
			if !ok {
				b.log(map[string]any{"event": "denied", "id": p.ID, "tool": name})
				return Result{}, fmt.Errorf("вызов %s отклонён человеком на хосте", name)
			}
		case <-time.After(ApprovalTimeout):
			b.log(map[string]any{"event": "approval-timeout", "id": p.ID, "tool": name})
			return Result{}, fmt.Errorf("вызов %s не подтверждён за %s (agctl dev bridge approve %s)", name, ApprovalTimeout, p.ID)
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	res := run(ctx, t, argv)
	b.log(map[string]any{"event": "call", "tool": name, "argv": argv, "exit": res.ExitCode, "ms": res.DurationMS, "error": res.Error})
	return res, nil
}

// PendingList — ожидающие подтверждения вызовы.
func (b *Bridge) PendingList() []Pending {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Pending, 0, len(b.pending))
	for _, p := range b.pending {
		out = append(out, Pending{ID: p.ID, Tool: p.Tool, Args: p.Args, Argv: p.Argv, Created: p.Created})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Decide подтверждает (approve=true) или отклоняет ожидающий вызов.
func (b *Bridge) Decide(id string, approve bool) error {
	b.mu.Lock()
	p := b.pending[id]
	b.mu.Unlock()
	if p == nil {
		return fmt.Errorf("ожидающий вызов %q не найден", id)
	}
	select {
	case p.verdict <- approve:
	default:
	}
	return nil
}

func run(ctx context.Context, t Tool, argv []string) Result {
	timeout := DefaultTimeout
	if t.TimeoutSec > 0 {
		timeout = time.Duration(t.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if t.Dir != "" {
		cmd.Dir = expandHome(t.Dir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, max: MaxOutput}
	cmd.Stderr = &limitedWriter{buf: &stderr, max: MaxOutput}
	started := time.Now()
	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(), Stderr: stderr.String(),
		DurationMS: time.Since(started).Milliseconds(),
		Truncated:  stdout.Len() >= MaxOutput || stderr.Len() >= MaxOutput,
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	case ctx.Err() == context.DeadlineExceeded:
		res.ExitCode, res.Error = -1, fmt.Sprintf("таймаут %s", timeout)
	default:
		res.ExitCode, res.Error = -1, err.Error()
	}
	return res
}

type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *Bridge) log(rec map[string]any) {
	if b.journal == "" {
		return
	}
	rec["ts"] = time.Now().Format(time.RFC3339)
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	b.jmu.Lock()
	defer b.jmu.Unlock()
	_ = os.MkdirAll(filepath.Dir(b.journal), 0o700)
	f, err := os.OpenFile(b.journal, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// SkillMarkdown — скилл агента (dsh-home/skills/agctl-host-bridge/SKILL.md):
// какие хост-команды доступны и как их вызывать изнутри контейнера.
func SkillMarkdown(tools []Tool) string {
	var sb strings.Builder
	sb.WriteString("---\nname: agctl-host-bridge\ndescription: Запуск разрешённых команд на компьютере пользователя (вне контейнера) через мост agctl. Используй, когда нужна программа хоста, которой нет в контейнере.\n---\n\n")
	sb.WriteString("# Мост к хосту\n\nТы работаешь в контейнере. Команды на компьютере пользователя доступны только\nчерез мост и только из белого списка ниже. Вызов:\n\n```\nagctl bridge call <инструмент> имя=значение ...\nagctl bridge list            # актуальный список\n```\n\nОтвет — JSON с exitCode, stdout, stderr. Инструменты с пометкой «нужно\nподтверждение» ждут, пока человек одобрит вызов на своей машине — скажи ему\nоб этом. Не пытайся обойти мост.\n\n## Инструменты\n\n")
	if len(tools) == 0 {
		sb.WriteString("Белый список пуст: владелец ещё не разрешил ни одной команды\n(`agctl dev bridge` на хосте, раздел bridge.tools в dev.yaml).\n")
	}
	for _, t := range tools {
		fmt.Fprintf(&sb, "### %s\n\n%s\n", t.Name, t.Description)
		if t.Confirm {
			sb.WriteString("\nНужно подтверждение человека.\n")
		}
		names := make([]string, 0, len(t.Args))
		for n := range t.Args {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a := t.Args[n]
			req := ""
			if a.Required {
				req = " (обязательный)"
			}
			fmt.Fprintf(&sb, "- `%s`%s — %s\n", n, req, a.Description)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
