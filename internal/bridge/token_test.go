package bridge

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

func TestExtractDSHBootToken(t *testing.T) {
	cases := map[string]string{
		"dsh web listening on http://127.0.0.1:3080/?token=abc_123-x": "abc_123-x",
		// The pattern matches dsh's own token charset [A-Za-z0-9_-], so the dot
		// stops the match — same semantics as the server-side boot-token regex.
		"see http://host/?a=1&token=Tok.9-b": "Tok",
		"no token here":                      "",
		"&token=plainSuffix":                 "plainSuffix",
	}
	for line, want := range cases {
		if got := ExtractDSHBootToken(line); got != want {
			t.Fatalf("Extract(%q)=%q, want %q", line, got, want)
		}
	}
}

func TestPumpDSHTokensForwardsNewTokensOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logs := `noise
boot url http://127.0.0.1:3080/?token=first
again http://127.0.0.1:3080/?token=first
empty line
http://127.0.0.1:3080/?token=second`
	tokens := make(chan string, 8)
	go PumpDSHTokens(ctx, stringsReader(logs), tokens)

	got := make([]string, 0, 2)
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < 2 && time.Now().Before(deadline) {
		select {
		case token := <-tokens:
			got = append(got, token)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("pumped %v; want [first second] without duplicates", got)
	}
}

func TestConnectorWebSocketURL(t *testing.T) {
	cases := map[string]string{
		"https://agctl.example":           "wss://agctl.example/api/local-backend/connect",
		"http://127.0.0.1:8929":           "ws://127.0.0.1:8929/api/local-backend/connect",
		"https://agctl.example/base/":     "wss://agctl.example/base/api/local-backend/connect",
		"https://user:pass@agctl.example": "", // rejected: embedded credentials
		"ftp://agctl.example":             "", // rejected scheme
	}
	for input, want := range cases {
		got, err := ConnectorWebSocketURL(input)
		if want == "" {
			if err == nil {
				t.Fatalf("ConnectorWebSocketURL(%q)=%q, want error", input, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Fatalf("ConnectorWebSocketURL(%q)=%q err=%v; want %q", input, got, err, want)
		}
	}
}
