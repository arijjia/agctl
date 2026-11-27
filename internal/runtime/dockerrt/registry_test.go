package dockerrt

import "testing"

// Реестр образов: локальное имя семейства agctl-dsh* → <реестр>/<имя>:<тег>
// (images.registry пусто — реестр ядра); "none" и ссылки с реестром — не
// перекладываются; чужие образы — тоже.
func TestRegistryRef(t *testing.T) {
	if got := ResolveRegistry(""); got != DefaultRegistry {
		t.Fatalf("пусто → %q, ждали %q", got, DefaultRegistry)
	}
	if got := ResolveRegistry(" none "); got != "" {
		t.Fatalf("none → %q", got)
	}
	if got := ResolveRegistry("registry.example.org:5000/team/"); got != "registry.example.org:5000/team" {
		t.Fatalf("свой реестр → %q", got)
	}
	cases := []struct{ ref, registry, want string }{
		{"agctl-dsh:0.2.2", ResolveRegistry(""), "ghcr.io/arijjia/agctl-dsh:0.2.2"},
		{"agctl-dsh-min:0.2.3-alpha.2", DefaultRegistry, "ghcr.io/arijjia/agctl-dsh-min:0.2.3-alpha.2"},
		{"agctl-dsh-web-org:0.2.2", "registry.example.org:5000/team", "registry.example.org:5000/team/agctl-dsh-web-org:0.2.2"},
		{"agctl-dsh", DefaultRegistry, "ghcr.io/arijjia/agctl-dsh:latest"},
		{"agctl-dsh:0.2.2", ResolveRegistry("none"), ""},
		{"ghcr.io/org/agctl-dsh:0.2.2", DefaultRegistry, ""},
		{"localhost:5000/agctl-dsh:1", DefaultRegistry, ""},
		{"ubuntu:24.04", DefaultRegistry, ""},
		{"evil/agctl-dsh:1", DefaultRegistry, ""},
	}
	for _, c := range cases {
		if got := RegistryRef(c.ref, c.registry); got != c.want {
			t.Errorf("RegistryRef(%q, %q) = %q, ждали %q", c.ref, c.registry, got, c.want)
		}
	}
	for ref, want := range map[string]bool{"agctl-dsh:0.2.2": true, "agctl-dsh-web-org:1": true, "ghcr.io/o/agctl-dsh:1": false, "node:22": false} {
		if got := LocalFamily(ref); got != want {
			t.Errorf("LocalFamily(%q) = %v", ref, got)
		}
	}
}
