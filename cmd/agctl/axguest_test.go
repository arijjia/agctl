package main

// axguest_test.go — guest-сервисы Substrate в раннере (spec.debug):
// --ax-debug-ssh поднимает process/filesystem gRPC на :80 (ax ssh).

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	atenenv "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestParseRunnerTaskDebug(t *testing.T) {
	task, err := parseRunnerTask(`
metadata: {name: agctl-chat-1}
spec:
  command: ["/usr/local/bin/agctl-entrypoint"]
  debug: true
`)
	if err != nil {
		t.Fatal(err)
	}
	if !task.Spec.Debug {
		t.Fatal("spec.debug must parse")
	}
}

func TestRunnerHandlerDebugServesGuestGRPC(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "dsh:"+r.URL.Path)
	}))
	defer backend.Close()
	target := strings.TrimPrefix(backend.URL, "http://")
	sup := newSupervisor([]string{"true"}, t.TempDir(), io.Discard)

	work := t.TempDir()
	handler, guestCleanup := runnerHandler(sup, target, "secret-token", true, work)
	if guestCleanup == nil {
		t.Fatal("debug task must build the guest server")
	}
	defer guestCleanup()

	// HTTP/1.1 probes/proxy и h2c gRPC на одном листенере — как раннер
	// апстрима AX (Protocols: HTTP1 + UnencryptedHTTP2).
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, Protocols: &protocols, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { _ = srv.Close() })

	// HTTP/1.1 по-прежнему работает (проба + прокси).
	resp, err := http.Get("http://" + lis.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}

	// gRPC guest-сервисов отвечает (process service отражён в proto
	// ateenv/v1alpha; ListProcesses живого трекера).
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := atenenv.NewProcessServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, callErr := client.GetProcess(ctx, &atenenv.GetProcessRequest{ProcessId: "no-such-process"})
	if callErr == nil {
		t.Fatal("unknown process must error, proving the gRPC service answers")
	}
	// Сервис вообще слушает: код ошибки gRPC (Unknown process), не HTTP-5xx
	// от прокси и не connection refused.
	if !strings.Contains(callErr.Error(), "codes.") && !strings.Contains(callErr.Error(), "process") {
		t.Fatalf("expected gRPC service error, got: %v", callErr)
	}
}

func TestRunnerHandlerDebugOffHasNoGuestCleanup(t *testing.T) {
	sup := newSupervisor([]string{"true"}, t.TempDir(), io.Discard)
	_, cleanup := runnerHandler(sup, "127.0.0.1:1", "secret-token", false, t.TempDir())
	if cleanup != nil {
		t.Fatal("debug off must not start guest services")
	}
}
