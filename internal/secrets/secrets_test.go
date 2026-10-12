package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	home := t.TempDir()
	keyPath, err := EnsureMasterKey(home)
	if err != nil {
		t.Fatalf("EnsureMasterKey: %v", err)
	}
	m, err := New(keyPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestEncryptDecryptRoundtrip — токен шифруется и расшифровывается без потерь.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	m := newTestManager(t)
	for _, plain := range []string{
		"sk-test-token",
		"очень длинный токен с юникодом и символами !@#$%^&*()_+",
		"",
	} {
		enc, err := m.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		got, err := m.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", enc, err)
		}
		if got != plain {
			t.Fatalf("roundtrip: дали %q, получили %q", plain, got)
		}
	}
}

// TestEncryptUniqueNonce — один и тот же токен даёт разные шифротексты
// (случайный nonce), и в шифротексте нет открытого текста.
func TestEncryptUniqueNonce(t *testing.T) {
	m := newTestManager(t)
	plain := "sk-same-token"
	a, _ := m.Encrypt(plain)
	b, _ := m.Encrypt(plain)
	if a == b {
		t.Fatal("два шифротекста совпали — nonce не случайный")
	}
	if strings.Contains(a, plain) {
		t.Fatal("открытый текст виден в шифротексте")
	}
}

// TestDecryptGarbage — битый/чужой шифротекст отклоняется.
func TestDecryptGarbage(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Decrypt("not-base64!!!"); err == nil {
		t.Fatal("битый base64 должен давать ошибку")
	}
	if _, err := m.Decrypt("QUJD"); err == nil { // "ABC" — короче nonce+tag
		t.Fatal("короткий шифротекст должен давать ошибку")
	}
	enc, _ := m.Encrypt("sk-token")
	if _, err := m.Decrypt(enc + "xx"); err == nil {
		t.Fatal("повреждённый шифротекст не должен расшифровываться")
	}
}

// TestWrongKey — ключ от другой установки не расшифровывает секреты.
func TestWrongKey(t *testing.T) {
	m1 := newTestManager(t)
	m2 := newTestManager(t) // другой мастер-ключ
	enc, err := m1.Encrypt("sk-tenant-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Decrypt(enc); err == nil {
		t.Fatal("чужой мастер-ключ не должен расшифровывать")
	}
}

// TestEnsureMasterKey — ключ создаётся один раз, права 0600, формат 32 байта.
func TestEnsureMasterKey(t *testing.T) {
	home := t.TempDir()
	p1, err := EnsureMasterKey(home)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	p2, err := EnsureMasterKey(home) // повторный вызов — тот же файл
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("путь изменился: %s → %s", p1, p2)
	}
	fi, err := os.Stat(p1)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("права ключа = %v, ждали 0600", fi.Mode().Perm())
	}
	key, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("длина ключа = %d байт, ждали 32 (AES-256)", len(key))
	}
	if _, err := New(filepath.Join(home, "нет-такого")); err == nil {
		t.Fatal("New без файла ключа должен падать")
	}
}
