package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMcpPresetCommandAndPassEnv(t *testing.T) {
	dsh := t.TempDir()
	var specs []McpServerSpec
	for _, id := range []string{"gitlab", "jira", "host-bridge", "ddg"} {
		s, ok := PlatformMcpServer(id)
		if !ok {
			t.Fatalf("нет %s в каталоге", id)
		}
		specs = append(specs, s)
	}
	env := map[string]string{
		"GITLAB_PERSONAL_ACCESS_TOKEN": "gl-tok", "GITLAB_API_URL": "https://g/api/v4",
		"JIRA_URL": "https://j", "JIRA_PERSONAL_TOKEN": "j-tok",
		"AGCTL_BRIDGE_URL": "http://host.docker.internal:8930", "AGCTL_BRIDGE_TOKEN": "br",
		"UNRELATED_SECRET": "must-not-leak",
	}
	if err := EnsureWorkspaceMcpPreset(dsh, "demo", specs, env); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(WorkspaceMcpPresetDir(dsh, "demo"), "agent.cordis.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"command: uvx", "mcp-atlassian==0.24.4", "command: agctl", "- bridge", "- mcp",
		"GITLAB_PERSONAL_ACCESS_TOKEN: gl-tok", "JIRA_PERSONAL_TOKEN: j-tok", "AGCTL_BRIDGE_TOKEN: br", "command: npx"} {
		if !strings.Contains(got, want) {
			t.Errorf("пресет без %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "must-not-leak") {
		t.Fatal("в пресет попадают только переменные из PassEnv выбранных серверов")
	}
	if strings.Contains(got, "- -y\n        - mcp-atlassian") {
		t.Fatal("-y — только для npx")
	}
}
