package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

// utmnView — профиль организации для проверок брендированного пути.
func utmnView(t *testing.T) profile.View {
	t.Helper()
	d, err := profile.Embedded("utmn")
	if err != nil {
		t.Fatal(err)
	}
	return d.View()
}

func TestChatShellRoleSpecificRightPane(t *testing.T) {
	doc := "<!doctype html><html><head></head><body></body></html>"
	srv := &Server{brand: profile.Neutral()}
	user := string(srv.injectChatShellForRole([]byte(doc), "user"))
	if !strings.Contains(user, `data-agctl="chat-shell-user"`) || !strings.Contains(user, `aria-label="Open right sidebar"`) {
		t.Fatal("ordinary user must get pre-paint right-panel hiding")
	}
	for _, role := range []string{"developer", "admin"} {
		got := string(srv.injectChatShellForRole([]byte(doc), role))
		if strings.Contains(got, `data-agctl="chat-shell-user"`) || strings.Contains(got, `aria-label="Open right sidebar"`) {
			t.Fatalf("role %s must keep the complete native right panel", role)
		}
		if !strings.Contains(got, "data-agctl-early-brand") {
			t.Fatalf("role %s lost the agctl brand pre-paint shell", role)
		}
	}
}

func TestInjectChatShell(t *testing.T) {
	srv := &Server{brand: utmnView(t)}
	shell := `<style data-agctl="chat-shell">`
	if !strings.Contains(chatShellHeadForRole("user", srv.brand), shell) {
		t.Fatal("chat shell head must contain the style marker")
	}

	// Обычный HTML dsh: инъекция после открывающего <head>, до любых скриптов.
	doc := "<!doctype html><html><head><meta charset='utf-8'><title>DeepSeek Harness</title></head><body><div id=root></div></body></html>"
	got := string(srv.injectChatShellForRole([]byte(doc), "user"))
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

	// HTML без head, но с body: вставка после открывающего <body…>.
	doc2 := "<html><body class='app'>x</body></html>"
	got2 := string(srv.injectChatShellForRole([]byte(doc2), "user"))
	if !strings.HasPrefix(got2, "<html><body class='app'>"+shell) {
		t.Fatalf("shell must follow <body…> tag: %q", got2[:80])
	}

	// Фрагмент без head/body/doctype остаётся поддержанным.
	got3 := string(srv.injectChatShellForRole([]byte("<div>x</div>"), "user"))
	if !strings.HasPrefix(got3, shell) {
		t.Fatal("shell must be prepended when no document structure exists")
	}

	// Идемпотентность: повторная инъекция ничего не меняет.
	if again := string(srv.injectChatShellForRole([]byte(got), "user")); again != got {
		t.Fatal("injection must be idempotent")
	}

	// Пустой документ — не трогаем.
	if empty := srv.injectChatShellForRole(nil, "user"); len(empty) != 0 {
		t.Fatal("empty body must stay empty")
	}
}

// TestInjectChatShellNeutralProfile — ядро без профиля организации брендится
// нейтральным agctl: никаких ТюмГУ ни в заголовке, ни в тэглайне.
func TestInjectChatShellNeutralProfile(t *testing.T) {
	srv := &Server{brand: profile.Neutral()}
	doc := "<!doctype html><html><head><title>DeepSeek Harness</title></head><body></body></html>"
	got := string(srv.injectChatShellForRole([]byte(doc), "user"))
	for _, org := range []string{"ТюмГУ", "Тюмень на правильном пути"} {
		if strings.Contains(got, org) {
			t.Fatalf("нейтральный шелл содержит %q", org)
		}
	}
	if !strings.Contains(got, "<title>agctl</title>") {
		t.Fatal("нейтральный профиль не подменил title на agctl")
	}
	if !strings.Contains(got, `"агентские рабочие места"`) {
		t.Fatal("нейтральный тэглайн не подставлен в шелл")
	}
}

func TestInjectChatShellHTMLAwarePlacement(t *testing.T) {
	srv := &Server{brand: utmnView(t)}
	doc := "<!DOCTYPE html><HTML><HEAD data-x='>'><!-- literal </head> --><script>const x = '</head>';</script><title>DeepSeek Harness</title></HEAD><BODY>x</BODY></HTML>"
	got := string(srv.injectChatShellForRole([]byte(doc), "user"))
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

func TestInjectBrandBootIntoLoginIndex(t *testing.T) {
	idx := []byte("<!doctype html><html><head><meta charset='utf-8'><title>agctl — рабочие места агентов</title></head><body><div id=root></div></body></html>")
	out := string(injectBrandBoot(idx, utmnView(t)))
	if !strings.Contains(out, `<script data-agctl="brand-boot">window.__AGCTL_BRAND__={`) {
		t.Fatal("brand boot script не вставлен в index")
	}
	if !strings.Contains(out, `"name":"ИИ-мастерская ТюмГУ"`) {
		t.Fatalf("brand JSON без имени организации: %s", out)
	}
	if !strings.Contains(out, "<title>ИИ-мастерская ТюмГУ</title>") {
		t.Fatal("title логин-страницы не заменён профилем")
	}
	// Инъекция до модульных скриптов и внутри <head>.
	boot := strings.Index(out, `data-agctl="brand-boot"`)
	headEnd := strings.Index(out, "</head>")
	if boot < 0 || headEnd < 0 || boot > headEnd {
		t.Fatalf("boot-скрипт вне <head>: boot=%d head=%d", boot, headEnd)
	}
	if again := string(injectBrandBoot([]byte(out), utmnView(t))); again != out {
		t.Fatal("brand boot инъекция должна быть идемпотентной")
	}
	// Документ без <head> — скрипт в начало.
	frag := string(injectBrandBoot([]byte("<div>x</div>"), profile.Neutral()))
	if !strings.HasPrefix(frag, `<script data-agctl="brand-boot">`) {
		t.Fatal("фрагмент без head должен получать скрипт префиксом")
	}
}

func TestInjectChatShellResponseUpdatesBodyLength(t *testing.T) {
	srv := &Server{brand: utmnView(t)}
	body := []byte("<!doctype html><html><head><title>DeepSeek Harness</title></head><body>x</body></html>")
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	srv.injectChatShellResponseForRole(resp, "user")
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
	srv := &Server{brand: utmnView(t)}
	body := bytes.Repeat([]byte("x"), chatShellMaxHTMLBytes+257)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"text/html"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	srv.injectChatShellResponseForRole(resp, "user")
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
	srv := &Server{brand: utmnView(t)}
	prefix := []byte("partial html")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       &failOnceBody{data: prefix},
	}
	srv.injectChatShellResponseForRole(resp, "user")
	got, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("original upstream read error must not be silently discarded")
	}
	if !bytes.Equal(got, prefix) {
		t.Fatalf("read-error prefix was not restored: %q", got)
	}
}
