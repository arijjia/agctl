package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestChatShellRoleSpecificRightPane(t *testing.T) {
	doc := "<!doctype html><html><head></head><body></body></html>"
	user := string(injectChatShellForRole([]byte(doc), "user"))
	if !strings.Contains(user, `data-agctl="chat-shell-user"`) || !strings.Contains(user, `aria-label="Open right sidebar"`) {
		t.Fatal("ordinary user must get pre-paint right-panel hiding")
	}
	for _, role := range []string{"developer", "admin"} {
		got := string(injectChatShellForRole([]byte(doc), role))
		if strings.Contains(got, `data-agctl="chat-shell-user"`) || strings.Contains(got, `aria-label="Open right sidebar"`) {
			t.Fatalf("role %s must keep the complete native right panel", role)
		}
		if !strings.Contains(got, `data-agctl="chat-shell-developer"`) || !strings.Contains(got, `data-agctl-developer-tools-ready="1"`) {
			t.Fatalf("role %s must collapse the empty right column until developer tools are ready", role)
		}
		if !strings.Contains(got, "data-agctl-early-brand") {
			t.Fatalf("role %s lost the agctl brand pre-paint shell", role)
		}
	}
}

func TestInjectChatShell(t *testing.T) {
	shell := `<style data-agctl="chat-shell">`
	if !strings.Contains(chatShellHead, shell) {
		t.Fatal("chat shell head must contain the style marker")
	}

	// Обычный HTML dsh: инъекция после открывающего <head>, до любых скриптов.
	doc := "<!doctype html><html><head><meta charset='utf-8'><title>DeepSeek Harness</title></head><body><div id=root></div></body></html>"
	got := string(injectChatShell([]byte(doc)))
	if strings.Count(got, shell) != 1 {
		t.Fatal("shell style not injected exactly once")
	}
	shellIdx := strings.Index(got, shell)
	headEnd := strings.Index(got, "</head>")
	if shellIdx == -1 || headEnd == -1 || shellIdx > headEnd {
		t.Fatalf("shell must be inside <head>: shell=%d head=%d", shellIdx, headEnd)
	}
	if !strings.Contains(got, "<title>ИИ-мастерская ТюмГУ</title>") {
		t.Fatal("document title must be branded before browser JS runs")
	}
	if !strings.Contains(got, "data-agctl-early-brand") || !strings.Contains(got, "Тюмень на правильном пути") {
		t.Fatal("document-start shell must render the platform brand/tagline before plugins")
	}
	if !strings.Contains(got, "function fixChrome(root){\n  var found;") {
		t.Fatal("document-start shell must declare fixChrome query state before strict-mode use")
	}

	// HTML без head, но с body: вставка после открывающего <body…>.
	doc2 := "<html><body class='app'>x</body></html>"
	got2 := string(injectChatShell([]byte(doc2)))
	if !strings.HasPrefix(got2, "<html><body class='app'>"+shell) {
		t.Fatalf("shell must follow <body…> tag: %q", got2[:80])
	}

	// Фрагмент без head/body/doctype остаётся поддержанным.
	got3 := string(injectChatShell([]byte("<div>x</div>")))
	if !strings.HasPrefix(got3, shell) {
		t.Fatal("shell must be prepended when no document structure exists")
	}

	// Идемпотентность: повторная инъекция ничего не меняет.
	if again := string(injectChatShell([]byte(got))); again != got {
		t.Fatal("injection must be idempotent")
	}

	// Пустой документ — не трогаем.
	if empty := injectChatShell(nil); len(empty) != 0 {
		t.Fatal("empty body must stay empty")
	}
}

func TestInjectChatShellHTMLAwarePlacement(t *testing.T) {
	doc := "<!DOCTYPE html><HTML><HEAD data-x='>'><!-- literal </head> --><script>const x = '</head>';</script><title>DeepSeek Harness</title></HEAD><BODY>x</BODY></HTML>"
	got := string(injectChatShell([]byte(doc)))
	if !strings.HasPrefix(got, "<!DOCTYPE html>") {
		t.Fatal("doctype moved; document may enter quirks mode")
	}
	marker := strings.Index(got, `<style data-agctl="chat-shell">`)
	script := strings.Index(got, "<script>const x")
	if marker < 0 || script < 0 || marker > script {
		t.Fatalf("shell must precede raw-text script: shell=%d script=%d", marker, script)
	}
	if strings.Count(got, "const x = '</head>';") != 1 {
		t.Fatal("inline script raw text was corrupted")
	}
	if !strings.Contains(got, "<title>ИИ-мастерская ТюмГУ</title>") {
		t.Fatal("mixed-case head document title was not branded")
	}
}

func TestInjectChatShellResponseUpdatesBodyLength(t *testing.T) {
	body := []byte("<!doctype html><html><head><title>DeepSeek Harness</title></head><body>x</body></html>")
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	injectChatShellResponse(resp)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`<style data-agctl="chat-shell">`)) {
		t.Fatal("shell was not injected into HTML response")
	}
	if resp.ContentLength != int64(len(got)) || resp.Header.Get("Content-Length") != strconv.Itoa(len(got)) {
		t.Fatalf("body length metadata mismatch: field=%d header=%q actual=%d", resp.ContentLength, resp.Header.Get("Content-Length"), len(got))
	}
}

func TestInjectChatShellResponseRestoresOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), chatShellMaxHTMLBytes+257)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"text/html"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	injectChatShellResponse(resp)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("oversized response changed/truncated: got=%d want=%d", len(got), len(body))
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatal("oversized response headers were modified")
	}
}

type failOnceBody struct {
	data []byte
	done bool
}

func (b *failOnceBody) Read(p []byte) (int, error) {
	if b.done {
		return 0, errors.New("upstream read failed")
	}
	b.done = true
	return copy(p, b.data), errors.New("upstream read failed")
}
func (*failOnceBody) Close() error { return nil }

func TestInjectChatShellResponseRestoresReadError(t *testing.T) {
	prefix := []byte("partial html")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       &failOnceBody{data: prefix},
	}
	injectChatShellResponse(resp)
	got, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("original upstream read error must not be silently discarded")
	}
	if !bytes.Equal(got, prefix) {
		t.Fatalf("read-error prefix was not restored: %q", got)
	}
}
