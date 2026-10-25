package server

import (
	"strings"
	"testing"
)

func TestRedactWorkspaceLogs(t *testing.T) {
	input := "url=http://127.0.0.1/?token=dsh-login-secret Authorization: Bearer bearer-secret AGCTL_LLM_API_KEY=llm-secret dsh-auth-cookie=cookie-secret api-key=sk-1234567890abcdef"
	got := redactWorkspaceLogs(input)
	for _, secret := range []string{"dsh-login-secret", "bearer-secret", "llm-secret", "cookie-secret", "sk-1234567890abcdef"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log secret %q leaked after redaction: %s", secret, got)
		}
	}
	if strings.Count(got, "[redacted]") != 5 {
		t.Fatalf("redacted log = %q; expected 5 redactions", got)
	}
}
