package server

import (
	"crypto/ed25519"
	"testing"

	"github.com/arijjia/agctl/internal/store"
	"golang.org/x/crypto/ssh"
)

func TestParseSSHPublicKey(t *testing.T) {
	pub := ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(ssh.MarshalAuthorizedKey(key))
	got, err := parseSSHPublicKey(valid)
	if err != nil || got != valid[:len(valid)-1] {
		t.Fatalf("valid key: got %q err=%v", got, err)
	}
	if _, err := parseSSHPublicKey("not-a-key"); err == nil {
		t.Fatal("malformed key accepted")
	}
	if _, err := parseSSHPublicKey(valid + valid); err == nil {
		t.Fatal("multiple keys in one value accepted")
	}
	if got, err := parseSSHPublicKey("  \n"); err != nil || got != "" {
		t.Fatalf("empty key: got=%q err=%v", got, err)
	}
}

func TestSSHHostPortFor(t *testing.T) {
	if got := sshHostPortFor(store.Workspace{SSHPort: 23000}); got != 0 {
		t.Fatalf("port without key = %d, want 0", got)
	}
	if got := sshHostPortFor(store.Workspace{SSHPubKey: "ssh-ed25519 ...", SSHPort: 23000}); got != 23000 {
		t.Fatalf("port with key = %d, want 23000", got)
	}
}
