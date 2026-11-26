package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Воркспейс по умолчанию на хостовом пути: в засеянном реестре добавляется
// запись и становится по умолчанию, прежний /workspace остаётся; повтор —
// без изменений; пустой dsh-home засевается сразу с этим путём.
func TestEnsureDefaultWorkspace(t *testing.T) {
	read := func(home string) map[string]any {
		raw, err := os.ReadFile(filepath.Join(home, "storages", "workspace.json"))
		if err != nil {
			t.Fatal(err)
		}
		var reg map[string]any
		if err := json.Unmarshal(raw, &reg); err != nil {
			t.Fatal(err)
		}
		return reg
	}
	defaultPath := func(reg map[string]any) string {
		g := reg["global"].(map[string]any)
		ws := reg["tables"].(map[string]any)["workspaces"].(map[string]any)
		return ws[g["defaultWorkspaceId"].(string)].(map[string]any)["path"].(string)
	}

	home := t.TempDir()
	if err := seedWorkspaceRegistry(home); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureDefaultWorkspace(home, "/Users/dev/src")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	reg := read(home)
	if got := defaultPath(reg); got != "/Users/dev/src" {
		t.Fatalf("по умолчанию %q", got)
	}
	ws := reg["tables"].(map[string]any)["workspaces"].(map[string]any)
	if ws["ws-agctl"].(map[string]any)["path"] != "/workspace" || len(ws) != 2 {
		t.Fatalf("прежний воркспейс должен остаться: %v", ws)
	}
	if changed, err := EnsureDefaultWorkspace(home, "/Users/dev/src"); changed || err != nil {
		t.Fatalf("повтор: changed=%v err=%v", changed, err)
	}
	// обратно на /workspace — существующая запись, новой не появляется
	if changed, err := EnsureDefaultWorkspace(home, "/workspace"); !changed || err != nil {
		t.Fatalf("возврат: changed=%v err=%v", changed, err)
	}
	if reg := read(home); defaultPath(reg) != "/workspace" || len(reg["tables"].(map[string]any)["workspaces"].(map[string]any)) != 2 {
		t.Fatalf("возврат на /workspace: %v", reg)
	}

	fresh := t.TempDir()
	if _, err := EnsureDefaultWorkspace(fresh, "/home/user/src"); err != nil {
		t.Fatal(err)
	}
	if got := defaultPath(read(fresh)); got != "/home/user/src" {
		t.Fatalf("пустой dsh-home: %q", got)
	}
}
