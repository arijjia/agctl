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
	if one.containerID == two.containerID || one.port == two.port {
		t.Fatalf("users share runtime unexpectedly: one=%+v two=%+v", one, two)
	}
	home1, _ := e.srv.userChatDirs(101)
	home2, _ := e.srv.userChatDirs(202)
	if home1 == home2 {
		t.Fatalf("users share persistent DSH_HOME: %s", home1)
	}
	otherDeployment := &Server{cfg: Config{Home: t.TempDir()}}
	if e.srv.chatRuntimeID(101) == otherDeployment.chatRuntimeID(101) {
		t.Fatal("separate agctl homes must not collide in Docker container names")
	}
	if _, err := os.Stat(filepath.Join(home1, "profiles", "web", "package.json")); err != nil {
		t.Fatalf("user volume not seeded: %v", err)
	}
	if e.rt.started[e.srv.chatRuntimeID(101)] != 1 || e.rt.started[e.srv.chatRuntimeID(202)] != 1 {
		t.Fatalf("start counts: %+v", e.rt.started)
	}
}

func TestReaperStopsIdleChatButKeepsPersistentVolume(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Second
	chat, err := e.srv.ensureChat(303)
	if err != nil {
		t.Fatal(err)
	}
	home, work := e.srv.userChatDirs(303)
	chat.lastSeen = time.Now().Add(-2 * time.Second)
	e.srv.reapIdleChats()
	if e.srv.chats[303] != nil {
		t.Fatal("idle runtime remains registered")
	}
	if e.rt.stopped[chat.containerID] != 1 {
		t.Fatalf("container stop count = %d, want 1", e.rt.stopped[chat.containerID])
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
	if e.srv.chats[404] == nil || e.rt.stopped[chat.containerID] != 0 {
		t.Fatal("reaper stopped an active chat request")
	}
}
