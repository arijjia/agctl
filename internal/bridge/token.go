package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// dshBootTokenRe — тот же вид boot-ссылки, что печатает dsh web при старте
// (совпадает с паттерном серверного mint transport).
var dshBootTokenRe = regexp.MustCompile(`[?&]token=([A-Za-z0-9_\-]+)`)

// ExtractDSHBootToken pulls the boot token out of one dsh web log line.
func ExtractDSHBootToken(line string) string {
	match := dshBootTokenRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// PumpDSHTokens scans a reader (local dsh web logs) and forwards every newly
// seen boot token to out. It stops on scanner end or ctx cancellation and
// never closes out.
func PumpDSHTokens(ctx context.Context, r io.Reader, out chan<- string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !scanner.Scan() {
			return
		}
		token := ExtractDSHBootToken(scanner.Text())
		if token == "" || token == last {
			continue
		}
		last = token
		select {
		case out <- token:
		case <-ctx.Done():
			return
		}
	}
}

// ConnectorWebSocketURL maps a platform origin to its bearer-only connector
// endpoint, e.g. https://agctl.example → wss://agctl.example/api/local-backend/connect.
func ConnectorWebSocketURL(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("bridge: invalid server URL %q", serverURL)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("bridge: server URL must use http(s), got %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/api/local-backend/connect"
	return parsed.String(), nil
}
