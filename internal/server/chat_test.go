package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
