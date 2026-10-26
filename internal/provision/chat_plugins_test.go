package provision_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
)

func TestEnsureChatPluginsMigratesOldChatHomeIdempotently(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	packageJSON := `{"name":"dsh-profile-web","private":true,"dependencies":{},"dsh":{"profile":{"bundles":["@deepseek-ai/dsh-base","@deepseek-ai/dsh-web-app"],"patchReload":"startup"}}}`
	if err := os.WriteFile(filepath.Join(profile, "package.json"), []byte(packageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := "- id: agent-default-model\n  name: \"@deepseek-ai/dsh-agent-default-model\"\n"
	if err := os.WriteFile(filepath.Join(profile, "cordis.patch.yml"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := provision.EnsureChatPlugins(home, assets.DshSkel); err != nil {
			t.Fatalf("EnsureChatPlugins attempt %d: %v", attempt+1, err)
		}
	}

	var manifest struct {
		DSH struct {
			Profile struct {
				Bundles []string `json:"bundles"`
			} `json:"profile"`
		} `json:"dsh"`
	}
	data, err := os.ReadFile(filepath.Join(profile, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, bundle := range []string{"@arijjia/agctl-cabinet", "@arijjia/agctl-chat-session-files", "@arijjia/agctl-chat-brand"} {
		count := 0
		for _, got := range manifest.DSH.Profile.Bundles {
			if got == bundle {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("bundle %s count = %d, want exactly 1: %v", bundle, count, manifest.DSH.Profile.Bundles)
		}
	}
	for _, rel := range []string{
		"node_modules/@arijjia/agctl-cabinet/lib/client.js",
		"node_modules/@arijjia/agctl-chat-session-files/lib/index.js",
		"node_modules/@arijjia/agctl-chat-brand/lib/client.js",
	} {
		if _, err := os.Stat(filepath.Join(profile, rel)); err != nil {
			t.Fatalf("managed chat plugin file %s missing: %v", rel, err)
		}
	}
	patchData, err := os.ReadFile(filepath.Join(profile, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bundle := range []string{"@arijjia/agctl-cabinet", "@arijjia/agctl-chat-session-files", "@arijjia/agctl-chat-brand"} {
		if count := strings.Count(string(patchData), bundle); count != 1 {
			t.Fatalf("cordis patch bundle %s count = %d, want 1", bundle, count)
		}
	}
	const accountDisable = "- id: ui-settings-account\n  name: \"@deepseek-ai/dsh-client-ui-settings-account\"\n  disabled: true"
	if count := strings.Count(string(patchData), accountDisable); count != 1 {
		t.Fatalf("account More/Get started disable patch count = %d, want 1", count)
	}
}

// dsh 0.2.x profile loader resolves the client entry as <bundle>/client.js and
// never sees the exports map. Legacy volumes also keep stale root files from
// ancient bundle layouts; sync must overwrite the entry (client bundles only)
// and prune the junk.
func TestEnsureChatPluginsWritesCompatEntryAndPrunesStaleFiles(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(filepath.Join(profile, "node_modules", "@arijjia", "agctl-chat-brand"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "package.json"), []byte(`{"dsh":{"profile":{"bundles":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(profile, "node_modules", "@arijjia", "agctl-chat-brand")
	staleRoot := filepath.Join(bundleDir, "client.js")
	staleLib := filepath.Join(bundleDir, "lib", "renamed-away.js")
	if err := os.MkdirAll(filepath.Dir(staleLib), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleRoot, []byte("// древняя «плоская» сборка, Сессии\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleLib, []byte("// файл, удалённый из скелета\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := provision.EnsureChatPlugins(home, assets.DshSkel); err != nil {
		t.Fatalf("EnsureChatPlugins: %v", err)
	}

	entry, err := os.ReadFile(staleRoot)
	if err != nil {
		t.Fatalf("compat client.js missing: %v", err)
	}
	lib, err := os.ReadFile(filepath.Join(bundleDir, "lib", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(entry) != string(lib) {
		t.Fatal("root client.js must be the compat entry copied from lib/client.js")
	}
	if strings.Contains(string(entry), "hideNativeSessionList") {
		t.Fatal("compat entry still serves the retired custom sidebar")
	}
	if _, err := os.Stat(staleLib); !os.IsNotExist(err) {
		t.Fatalf("stale lib file survived prune: %v", err)
	}
	// Host-only bundle: client entry is neither required nor created.
	if _, err := os.Stat(filepath.Join(profile, "node_modules", "@arijjia", "agctl-chat-session-files", "client.js")); !os.IsNotExist(err) {
		t.Fatalf("host-only bundle must not gain a client entry: %v", err)
	}
}
