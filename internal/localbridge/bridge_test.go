package localbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goTool — инструмент на бинаре go (есть на всех CI-ОС): go env NAME.
func goTool(confirm bool) Tool {
	return Tool{
		Name: "go_env", Description: "значение переменной go env",
		Command: []string{"go", "env", "{{name}}"},
		Args:    map[string]Arg{"name": {Description: "имя", Required: true, Pattern: `[A-Z]+`}},
		Confirm: confirm, TimeoutSec: 60,
	}
}

func TestValidateRejectsBadTools(t *testing.T) {
	cases := map[string]Tool{
		"имя":             {Name: "Bad-Name", Command: []string{"x"}},
		"пустая команда":  {Name: "x"},
		"плейсхолдер exe": {Name: "x", Command: []string{"{{a}}"}, Args: map[string]Arg{"a": {}}},
		"неописанный":     {Name: "x", Command: []string{"x", "{{a}}"}},
		"битый pattern":   {Name: "x", Command: []string{"x", "{{a}}"}, Args: map[string]Arg{"a": {Pattern: "("}}},
	}
	for name, tool := range cases {
		if err := Validate([]Tool{tool}); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
	if err := Validate([]Tool{goTool(false), goTool(false)}); err == nil {
		t.Error("дубль имени должен отклоняться")
	}
	if err := Validate([]Tool{goTool(false)}); err != nil {
		t.Fatal(err)
	}
}

func TestExpandNoInjection(t *testing.T) {
	tool := Tool{Name: "check", Command: []string{"ibcmd", "--db-path={{db}}", "{{opt}}"},
		Args: map[string]Arg{"db": {Required: true}, "opt": {}}}
	argv, err := tool.Expand(map[string]string{"db": `C:\1c\ib; rm -rf /`})
	if err != nil {
		t.Fatal(err)
	}
	// значение целиком в одном элементе argv, shell нет; пустой опц. выпал
	if len(argv) != 2 || argv[1] != `--db-path=C:\1c\ib; rm -rf /` {
		t.Fatalf("argv=%q", argv)
	}
	if _, err := tool.Expand(map[string]string{"db": "--config=/etc/x"}); err == nil {
		t.Fatal("значение с ведущим «-» (подстановка опции) должно отклоняться дефолтной проверкой")
	}
	if _, err := tool.Expand(map[string]string{"db": "a\nb"}); err == nil {
		t.Fatal("управляющие символы должны отклоняться")
	}
	if _, err := tool.Expand(map[string]string{}); err == nil {
		t.Fatal("обязательный аргумент")
	}
	if _, err := tool.Expand(map[string]string{"db": "x", "evil": "1"}); err == nil {
		t.Fatal("неизвестный аргумент должен отклоняться")
	}
}

func TestCallRunsAndJournals(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "bridge.log")
	b, err := New([]Tool{goTool(false)}, journal)
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.Call(context.Background(), "go_env", map[string]string{"name": "GOARCH"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) == "" {
		t.Fatalf("res=%+v", res)
	}
	if _, err := b.Call(context.Background(), "nope", nil); err == nil {
		t.Fatal("инструмент вне белого списка")
	}
	if _, err := b.Call(context.Background(), "go_env", map[string]string{"name": "go; ls"}); err == nil {
		t.Fatal("pattern [A-Z]+ должен отклонить значение")
	}
	raw, _ := os.ReadFile(journal)
	if !strings.Contains(string(raw), `"tool":"go_env"`) || strings.Contains(string(raw), "stdout") {
		t.Fatalf("журнал: вызов без stdout, получено %s", raw)
	}
}

func TestConfirmFlow(t *testing.T) {
	b, err := New([]Tool{goTool(true)}, "")
	if err != nil {
		t.Fatal(err)
	}
	type out struct {
		res Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, e := b.Call(context.Background(), "go_env", map[string]string{"name": "GOOS"})
		done <- out{r, e}
	}()
	var pending []Pending
	for i := 0; i < 100 && len(pending) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		pending = b.PendingList()
	}
	if len(pending) != 1 {
		t.Fatalf("ожидался один вызов на подтверждении, есть %d", len(pending))
	}
	if err := b.Decide(pending[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if o := <-done; o.err == nil {
		t.Fatal("отклонённый вызов не должен исполняться")
	}
	go func() {
		r, e := b.Call(context.Background(), "go_env", map[string]string{"name": "GOOS"})
		done <- out{r, e}
	}()
	pending = nil
	for i := 0; i < 100 && len(pending) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		pending = b.PendingList()
	}
	_ = b.Decide(pending[0].ID, true)
	if o := <-done; o.err != nil || o.res.ExitCode != 0 {
		t.Fatalf("подтверждённый вызов: %+v %v", o.res, o.err)
	}
}

func TestClientAndMCP(t *testing.T) {
	b, err := New([]Tool{goTool(false)}, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case PathTools:
			_ = json.NewEncoder(w).Encode(map[string]any{"tools": b.Tools()})
		case PathCall:
			var req CallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			res, err := b.Call(r.Context(), req.Tool, req.Args)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(res)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "tok"}
	tools, err := c.List(context.Background())
	if err != nil || len(tools) != 1 || len(tools[0].Command) != 0 {
		t.Fatalf("клиенту не отдаётся argv: %+v %v", tools, err)
	}
	if _, err := (&Client{URL: srv.URL, Token: "bad"}).List(context.Background()); err == nil {
		t.Fatal("неверный токен")
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { _ = ServeMCP(context.Background(), inR, outW, c) }()
	rd := bufio.NewReader(outR)
	send := func(msg string) map[string]any {
		_, _ = io.WriteString(inW, msg+"\n")
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	init := send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if init["result"].(map[string]any)["serverInfo"] == nil {
		t.Fatalf("initialize: %v", init)
	}
	_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	list := send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tl := list["result"].(map[string]any)["tools"].([]any)
	if len(tl) != 1 || tl[0].(map[string]any)["name"] != "go_env" {
		t.Fatalf("tools/list: %v", list)
	}
	call := send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"go_env","arguments":{"name":"GOOS"}}}`)
	res := call["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("tools/call: %v", call)
	}
	bad := send(`{"jsonrpc":"2.0","id":4,"method":"nope"}`)
	if bad["error"] == nil {
		t.Fatal("неизвестный метод — ошибка JSON-RPC")
	}
	_ = inW.Close()
}

func TestSkillMarkdownListsTools(t *testing.T) {
	md := SkillMarkdown([]Tool{goTool(true)})
	for _, want := range []string{"name: agctl-host-bridge", "agctl bridge call", "### go_env", "подтверждение"} {
		if !strings.Contains(md, want) {
			t.Fatalf("скилл без %q:\n%s", want, md)
		}
	}
	if !strings.Contains(SkillMarkdown(nil), "пуст") {
		t.Fatal("пустой белый список — честная подсказка")
	}
}
