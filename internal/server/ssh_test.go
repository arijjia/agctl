package server

import (
	"crypto/ed25519"
	"strings"
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

func TestWorkspaceSSHKeyProvision(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "ssh-test", "ssh-test-pass")
	pub := ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))
	sshKey, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshKey)))
	req := validCreate("ssh-enabled")
	req["sshPubKey"] = pubLine
	code, body := e.do(e.admin, "POST", "/api/workspaces", req)
	if code != 202 {
		t.Fatalf("create with ssh key = %d %s", code, body)
	}
	id := wsIDof(t, body)
	ws := e.waitStatus(id, "running")
	if ws.SSHPubKey != pubLine || ws.SSHPort == 0 {
		t.Fatalf("ssh fields not persisted: key=%q port=%d", ws.SSHPubKey, ws.SSHPort)
	}
	opts := e.rt.startOpts[id]
	if opts.SSHPubKey != pubLine || opts.SSHHostPort != ws.SSHPort {
		t.Fatalf("SSH opts not passed to runtime: %+v, workspace port=%d", opts, ws.SSHPort)
	}
	code, body = e.do(e.admin, "GET", "/api/workspaces/"+id, nil)
	if code != 200 || !strings.Contains(body, `"sshEnabled":true`) || !strings.Contains(body, `"sshPort":`) {
		t.Fatalf("ssh info missing from workspace API: %d %s", code, body)
	}
}

func TestWorkspaceSSHIsOwnerOnly(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "ssh-owner", "ssh-owner-pass")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("ssh-owner-ws"))
	if code != 202 {
		t.Fatalf("create: %d %s", code, body)
	}
	id := wsIDof(t, body)
	e.waitStatus(id, "running")
	bob := e.newClient()
	e.register(bob, "ssh-other", "ssh-other-pass")
	code, body = e.do(bob, "PATCH", "/api/workspaces/"+id+"/ssh", map[string]string{"pubKey": ""})
	if code != 403 {
		t.Fatalf("foreign SSH key mutation: %d %s, want 403", code, body)
	}
}

func TestWorkspaceSSHPatchRejectsMalformedKey(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "ssh-patch", "ssh-patch-pass")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("ssh-patch"))
	if code != 202 {
		t.Fatalf("create = %d %s", code, body)
	}
	id := wsIDof(t, body)
	e.waitStatus(id, "running")
	code, body = e.do(e.admin, "PATCH", "/api/workspaces/"+id+"/ssh", map[string]string{"pubKey": "bad-key"})
	if code != 400 || !strings.Contains(body, "некорректный") {
		t.Fatalf("malformed key: %d %s", code, body)
	}
}
