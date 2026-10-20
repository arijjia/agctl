package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
)

func TestEnsureChatIsPerUserAndReusesRuntime(t *testing.T) {
	e := newTestEnv(t, 3)
	one, err := e.srv.ensureChat(101)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.srv.ensureChat(101)
	if err != nil {
		t.Fatal(err)
	}
	two, err := e.srv.ensureChat(202)
	if err != nil {
		t.Fatal(err)
	}
	if one != again {
		t.Fatal("same user should reuse one live chat runtime")
	}
	if one.handle.RuntimeID == two.handle.RuntimeID || one.handle.Endpoint == two.handle.Endpoint {
		t.Fatalf("users share runtime unexpectedly: one=%+v two=%+v", one.handle, two.handle)
	}
	home1 := filepath.Join(e.home, "chats", "u101", "dsh-home")
	home2 := filepath.Join(e.home, "chats", "u202", "dsh-home")
	if home1 == home2 {
		t.Fatalf("users share persistent DSH_HOME: %s", home1)
	}
	otherDeployment := &Server{cfg: Config{Home: t.TempDir()}}
	if e.srv.chatRuntimeID(101) == otherDeployment.chatRuntimeID(101) {
		t.Fatal("separate agctl homes must not collide in runtime ids")
	}
	if _, err := os.Stat(filepath.Join(home1, "profiles", "web", "package.json")); err != nil {
		t.Fatalf("user volume not seeded: %v", err)
	}
	if e.rt.chatStarts[e.srv.chatRuntimeID(101)] != 1 || e.rt.chatStarts[e.srv.chatRuntimeID(202)] != 1 {
		t.Fatalf("chat start counts: %+v", e.rt.chatStarts)
	}
}

func TestReaperStopsIdleChatButKeepsPersistentVolume(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Second
	chat, err := e.srv.ensureChat(303)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(e.home, "chats", "u303")
	home, work := filepath.Join(base, "dsh-home"), filepath.Join(base, "work")
	chat.lastSeen = time.Now().Add(-2 * time.Second)
	e.srv.reapIdleChats()
	if e.srv.chats[303] != nil {
		t.Fatal("idle runtime remains registered")
	}
	if e.rt.chatStops[chat.handle.RuntimeID] != 1 {
		t.Fatalf("chat stop count = %d, want 1", e.rt.chatStops[chat.handle.RuntimeID])
	}
	for _, dir := range []string{home, work} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("persistent volume path %s was removed: %v", dir, err)
		}
	}
}

func TestChatUsesPlatformDefaultProviderWhenNoPersonalSecret(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.DefaultLLM = provision.Provider{
		BaseURL: "https://llm.example.invalid/v1", Model: "qwen-3.8-fast",
		Models: []string{"qwen-3.8-fast", "qwen-3.8-27b"}, APIKey: "platform-key-xyz",
	}
	if _, err := e.srv.ensureChat(606); err != nil {
		t.Fatal(err)
	}
	got := e.rt.chatSpecs[e.srv.chatRuntimeID(606)].Provider
	if got.BaseURL != e.srv.cfg.DefaultLLM.BaseURL || got.APIKey != "platform-key-xyz" || len(got.Models) != 2 {
		t.Fatalf("chat provider = %+v, want platform default", got)
	}
	if got.Model != "qwen-3.8-fast" {
		t.Fatalf("default model = %q, want qwen-3.8-fast", got.Model)
	}
}

func TestPersonalSecretOverridesPlatformDefault(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "https://corp.example.invalid/v1", Model: "corp-model", APIKey: "corp-key"}
	cipher, err := e.srv.sec.Encrypt("personal-key")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := e.srv.db.CreateUser("provider-user", "h", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.srv.db.SecretUpsert(uid, "https://personal.example.invalid/v1", "my-model", []string{"my-model"}, cipher); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.ensureChat(uid); err != nil {
		t.Fatal(err)
	}
	got := e.rt.chatSpecs[e.srv.chatRuntimeID(uid)].Provider
	if got.BaseURL != "https://personal.example.invalid/v1" || got.APIKey != "personal-key" || got.Model != "my-model" {
		t.Fatalf("chat provider = %+v, want personal secret to win", got)
	}
}

func TestChatEnsureRefreshesProviderOnRestartedRuntimeHome(t *testing.T) {
	e := newTestEnv(t, 3)
	// DSH_HOME, засеянный старой версией agctl без платформенного провайдера.
	home := filepath.Join(e.home, "chats", "u707", "dsh-home")
	if err := provision.SeedDshHome(home, provision.Provider{}, assetsDshSkel); err != nil {
		t.Fatal(err)
	}
	e.srv.cfg.DefaultLLM = provision.Provider{
		BaseURL: "https://corp.example.invalid/v1", Model: "qwen-3.8-fast", APIKey: "corp-key",
	}
	if _, err := e.srv.ensureChat(707); err != nil {
		t.Fatal(err)
	}
	creds, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(creds), "AGCTL_LLM_API_KEY: ") {
		t.Fatalf("restarted home was not refreshed with platform provider: %s", creds)
	}
}

func TestChatTokenRefreshesAfterPodOrContainerRestart(t *testing.T) {
	e := newTestEnv(t, 3)
	chat, err := e.srv.ensureChat(505)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.srv.chatToken(context.Background(), chat.handle); got != "fake-chat-"+chat.handle.RuntimeID {
		t.Fatalf("initial boot token = %q", got)
	}
	e.rt.logs[chat.handle.InstanceID] = "dsh web listening on http://127.0.0.1/?token=refreshed-token"
	if got := e.srv.chatToken(context.Background(), chat.handle); got != "refreshed-token" {
		t.Fatalf("boot token after restart = %q, want refreshed token", got)
	}
}

func TestReaperDoesNotStopActiveChat(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Second
	chat, err := e.srv.ensureChat(404)
	if err != nil {
		t.Fatal(err)
	}
	chat.lastSeen = time.Now().Add(-time.Hour)
	chat.active = 1
	e.srv.reapIdleChats()
	if e.srv.chats[404] == nil || e.rt.chatStops[chat.handle.RuntimeID] != 0 {
		t.Fatal("reaper stopped an active chat request")
	}
}
