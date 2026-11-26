package dockerrt

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Сборка на месте под именем реестра — только когда реестр сказал «нет
// такого образа» (или пакет закрыт); сбой связи и отмена — нет.
func TestPullMissing(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New(`pull ghcr.io/o/agctl-dsh:9.9: Error response from daemon: manifest unknown`), true},
		{errors.New(`Error response from daemon: Head "https://ghcr.io/v2/o/agctl-dsh/manifests/0.2.2": denied`), true},
		{errors.New(`pull access denied for agctl-dsh, repository does not exist or may require 'docker login'`), true},
		{errors.New(`Error response from daemon: unauthorized: authentication required`), true},
		{errors.New(`Get "https://ghcr.io/v2/": dial tcp: lookup ghcr.io: no such host`), false},
		{errors.New(`Get "https://ghcr.io/v2/": net/http: TLS handshake timeout`), false},
		{fmt.Errorf("pull x: %w", context.DeadlineExceeded), false},
		{fmt.Errorf("pull x: %w", context.Canceled), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := PullMissing(c.err); got != c.want {
			t.Errorf("PullMissing(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
