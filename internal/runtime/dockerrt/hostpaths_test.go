package dockerrt

import (
	"reflect"
	"testing"
)

// Пути как на хосте: по своему пути монтируются только обычные папки
// пользователя; системные каталоги образа, Windows-пути и пути с «:» — нет.
func TestHostPathMountable(t *testing.T) {
	for p, want := range map[string]bool{
		"/Users/dev/src":              true,
		"/home/user/src":              true,
		"/Volumes/data/repos":         true,
		"/private/var/folders/x/proj": true,
		"/":                           false,
		"/workspace":                  false,
		"/workspace/x":                false,
		"/home/dsh":                   false,
		"/usr/local/src":              false,
		"/tmp/proj":                   true,
		"/var/folders/x/T/proj":       true,
		"/tmp":                        false,
		"/tmp/npm-cache/x":            false,
		"/home":                       false,
		"/opt/dsh-skel":               false,
		"/opt/tools":                  true,
		"/var/lib/x":                  false,
		`C:\Users\dev\src`:            false,
		"relative/src":                false,
		"/Users/dev/a:b":              false,
	} {
		if got := HostPathMountable(p); got != want {
			t.Errorf("%s → %v, нужно %v", p, got, want)
		}
	}
}

func TestOverrideEnv(t *testing.T) {
	base := containerEnv("ws1", []string{"JIRA_URL=x"})
	got := overrideEnv(base, []string{"HOME=/Users/dev/src", "AGCTL_WORKSPACE_ROOT=/Users/dev/src"})
	want := []string{"AGCTL_WS_ID=ws1", "NPM_CONFIG_CACHE=/tmp/npm-cache", "XDG_CACHE_HOME=/tmp/cache", "JIRA_URL=x",
		"HOME=/Users/dev/src", "AGCTL_WORKSPACE_ROOT=/Users/dev/src"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env:\n%v\nнужно\n%v", got, want)
	}
	if got := overrideEnv(base, nil); !reflect.DeepEqual(got, base) {
		t.Fatalf("без перекрытий env меняться не должен: %v", got)
	}
}
