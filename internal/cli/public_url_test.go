package cli

import "testing"

func TestDerivePublicURL(t *testing.T) {
	cases := []struct {
		public, addr, want string
	}{
		{"https://agctl.corp.invalid/", "127.0.0.1:8929", "https://agctl.corp.invalid"},
		{"", "10.0.0.5:8929", "http://10.0.0.5:8929"},
		{"", "[::1]:8929", "http://[::1]:8929"},
		{"", "no-port", "no-port"},
	}
	for _, c := range cases {
		if got := DerivePublicURL(c.public, c.addr); got != c.want {
			t.Fatalf("DerivePublicURL(%q,%q) = %q; want %q", c.public, c.addr, got, c.want)
		}
	}
}
