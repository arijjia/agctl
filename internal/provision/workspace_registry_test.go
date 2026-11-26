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
	if ws["ws-agctl"].(map[string]any)["title"] != "/workspace" || ws["ws-agctl-host"].(map[string]any)["title"] != "default-workspace" {
		t.Fatalf("названия: прежний — по пути, новый — по умолчанию: %v", ws)
	}
	if changed, err := EnsureDefaultWorkspace(home, "/Users/dev/src"); changed || err != nil {
		t.Fatalf("повтор: changed=%v err=%v", changed, err)
	}
	// название, данное владельцем, повторный старт без смены не трогает
	reg = read(home)
	reg["tables"].(map[string]any)["workspaces"].(map[string]any)["ws-agctl-host"].(map[string]any)["title"] = "Мои проекты"
	raw, _ := json.Marshal(reg)
	if err := os.WriteFile(filepath.Join(home, "storages", "workspace.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := EnsureDefaultWorkspace(home, "/Users/dev/src"); changed || err != nil {
		t.Fatalf("повтор после переименования: changed=%v err=%v", changed, err)
	}
	if title := read(home)["tables"].(map[string]any)["workspaces"].(map[string]any)["ws-agctl-host"].(map[string]any)["title"]; title != "Мои проекты" {
		t.Fatalf("название владельца сброшено: %v", title)
	}
	// обратно на /workspace — существующая запись, новой не появляется
	if changed, err := EnsureDefaultWorkspace(home, "/workspace"); !changed || err != nil {
		t.Fatalf("возврат: changed=%v err=%v", changed, err)
	}
	if reg := read(home); defaultPath(reg) != "/workspace" || len(reg["tables"].(map[string]any)["workspaces"].(map[string]any)) != 2 {
		t.Fatalf("возврат на /workspace: %v", reg)
	}

	// отпустить хостовый воркспейс: свой — возвращается на /workspace,
	// выбор владельца — нет, нет реестра — нечего делать
	if changed, err := EnsureDefaultWorkspace(home, "/Users/dev/src"); !changed || err != nil {
		t.Fatalf("снова на хост: changed=%v err=%v", changed, err)
	}
	if changed, err := ReleaseHostWorkspace(home, "/workspace"); !changed || err != nil || defaultPath(read(home)) != "/workspace" {
		t.Fatalf("отпустить свой хостовый: changed=%v err=%v", changed, err)
	}
	reg = read(home)
	reg["global"].(map[string]any)["defaultWorkspaceId"] = "ws-mine"
	reg["tables"].(map[string]any)["workspaces"].(map[string]any)["ws-mine"] = map[string]any{"path": "/workspace/app", "title": "app"}
	raw, _ = json.Marshal(reg)
	if err := os.WriteFile(filepath.Join(home, "storages", "workspace.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ReleaseHostWorkspace(home, "/workspace"); changed || err != nil || defaultPath(read(home)) != "/workspace/app" {
		t.Fatalf("выбор владельца тронут: changed=%v err=%v", changed, err)
	}
	if changed, err := ReleaseHostWorkspace(t.TempDir(), "/workspace"); changed || err != nil {
		t.Fatalf("без реестра: changed=%v err=%v", changed, err)
	}

	fresh := t.TempDir()
	if _, err := EnsureDefaultWorkspace(fresh, "/home/user/src"); err != nil {
		t.Fatal(err)
	}
	if got := defaultPath(read(fresh)); got != "/home/user/src" {
		t.Fatalf("пустой dsh-home: %q", got)
	}
}
