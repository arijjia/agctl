package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseRunnerTaskIsLenient(t *testing.T) {
	task, err := parseRunnerTask(`
apiVersion: ax.io/v1alpha1
kind: Task
metadata: {name: agctl-chat-1, atespace: agctl}
spec:
  image: reg.invalid/x@sha256:abc
  command: ["/usr/local/bin/agctl-entrypoint"]
  env:
    - {name: A, value: "1"}
  debug: false
  config: {agctlSpecHash: deadbeef}
  someFutureField: {nested: true}
`)
	if err != nil {
		t.Fatal(err)
	}
	if task.Metadata.Name != "agctl-chat-1" || task.Spec.Command[0] != defaultRunnerCommand[0] || task.Spec.Env[0].Name != "A" {
		t.Fatalf("unexpected task: %+v", task)
	}
	if empty, err := parseRunnerTask(""); err != nil || len(empty.Spec.Command) != 0 {
		t.Fatalf("empty task must be allowed: %+v %v", empty, err)
	}
	if _, err := parseRunnerTask("spec: [unclosed"); err == nil {
		t.Fatal("broken YAML must be an error")
	}
}

func TestRunnerHandlerProbesProxyAndBootLog(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Ate-Target-Actor") != "" {
			http.Error(w, "router/runner headers leaked to dsh", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "dsh:"+r.URL.Path)
	}))
	defer backend.Close()
	target := strings.TrimPrefix(backend.URL, "http://")
	sup := newSupervisor([]string{"true"}, t.TempDir(), io.Discard)
	sup.append([]byte("dsh web listening on http://10.0.0.1:3080/?token=boot-xyz"))
	srv := httptest.NewServer(runnerHandler(sup, target, "secret-token"))
	defer srv.Close()

	get := func(path, auth string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Ate-Target-Actor", "agctl/x")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, _ := get("/healthz", ""); code != http.StatusOK {
		t.Fatalf("healthz = %d", code)
	}
	if code, _ := get("/readyz", ""); code != http.StatusOK {
		t.Fatalf("readyz with live backend = %d", code)
	}
	if code, body := get("/some/dsh/path", "Bearer other"); code != http.StatusOK || body != "dsh:/some/dsh/path" {
		t.Fatalf("proxy = %d %q", code, body)
	}
	if code, _ := get("/agctl/boot-log", ""); code != http.StatusNotFound {
		t.Fatalf("boot log without token = %d, want 404", code)
	}
	if code, _ := get("/agctl/boot-log", "Bearer wrong"); code != http.StatusNotFound {
		t.Fatalf("boot log with wrong token = %d, want 404", code)
	}
	if code, body := get("/agctl/boot-log?tail=1", "Bearer secret-token"); code != http.StatusOK || !strings.Contains(body, "token=boot-xyz") {
		t.Fatalf("boot log = %d %q", code, body)
	}
	// Без токена раннера эндпоинт выключен полностью.
	off := httptest.NewServer(runnerHandler(sup, target, ""))
	defer off.Close()
	resp, err := http.Get(off.URL + "/agctl/boot-log")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("boot log without configured token = %d, want 404", resp.StatusCode)
	}
	backend.Close()
	if code, _ := get("/readyz", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with dead backend = %d, want 503", code)
	}
}

func TestSupervisorTailKeepsLastLines(t *testing.T) {
	sup := newSupervisor([]string{"true"}, t.TempDir(), io.Discard)
	for i := 0; i < 5; i++ {
		sup.append([]byte("line" + string(rune('0'+i))))
	}
	if got := sup.tail(2); got != "line3\nline4\n" {
		t.Fatalf("tail(2) = %q", got)
	}
	if got := sup.tail(0); !strings.HasPrefix(got, "line0\n") {
		t.Fatalf("tail(0) = %q", got)
	}
	big := strings.Repeat("x", runnerLogLimit)
	sup.append([]byte(big))
	sup.append([]byte("last"))
	if got := sup.tail(1); got != "last\n" {
		t.Fatalf("ring buffer must keep the newest line: %q", got[:min(len(got), 20)])
	}
}

func TestSupervisorCapturesOutputAndRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based supervisor test")
	}
	sup := newSupervisor([]string{"sh", "-c", "echo boot-$$; sleep 30"}, t.TempDir(), io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(sup.tail(0), "boot-") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(sup.tail(0), "boot-") {
		t.Fatal("child output was not captured")
	}
	cancel()
	done := make(chan struct{})
	go func() { sup.wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(runnerStopGrace + 5*time.Second):
		t.Fatal("supervisor did not stop after cancel")
	}
}
