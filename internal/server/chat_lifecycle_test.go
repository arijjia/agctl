package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestStoppedChatNotRevivedByProxy — место, остановленное владельцем, прокси
// не поднимает (открытая вкладка dsh переподключается), а явный старт —
// поднимает.
func TestStoppedChatNotRevivedByProxy(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	_, uid := ownerClient(t, e)
	rid := e.srv.chatRuntimeID(uid, 0)
	if _, err := e.srv.startChat(uid, 0); err != nil {
		t.Fatal(err)
	}
	had, err := e.srv.stopChatKey(chatKey{uid, 0}, true, "тест")
	if err != nil || !had {
		t.Fatalf("stop: had=%v err=%v", had, err)
	}
	if e.rt.chatStops[rid] != 1 {
		t.Fatalf("StopChat вызван %d раз, ждали 1", e.rt.chatStops[rid])
	}
	before := e.rt.chatStarts[rid]
	if _, err := e.srv.ensureChatForProxy(context.Background(), uid, 0, time.Second); !errors.Is(err, errChatStopped) {
		t.Fatalf("прокси: err=%v, ждали errChatStopped", err)
	}
	if _, err := e.srv.ensureChat(uid, 0); !errors.Is(err, errChatStopped) {
		t.Fatalf("ensureChat: err=%v, ждали errChatStopped", err)
	}
	if e.rt.chatStarts[rid] != before {
		t.Fatalf("остановленное место поднялось: стартов %d → %d", before, e.rt.chatStarts[rid])
	}
	if _, err := e.srv.startChat(uid, 0); err != nil {
		t.Fatalf("явный старт: %v", err)
	}
	if e.rt.chatStarts[rid] != before+1 {
		t.Fatalf("явный старт не поднял место")
	}
	if _, err := e.srv.ensureChatForProxy(context.Background(), uid, 0, time.Second); err != nil {
		t.Fatalf("после явного старта прокси: %v", err)
	}
}

// TestInternalStopKeepsAutostart — остановка для перезапуска (не владельцем)
// не запрещает подъём.
func TestInternalStopKeepsAutostart(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	_, uid := ownerClient(t, e)
	if _, err := e.srv.startChat(uid, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.stopChatKey(chatKey{uid, 0}, false, "перезапуск"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.ensureChatForProxy(context.Background(), uid, 0, 5*time.Second); err != nil {
		t.Fatalf("после внутренней остановки прокси должен поднять место: %v", err)
	}
}
