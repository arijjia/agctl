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
}
