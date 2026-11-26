package provision

// workspace_registry.go — воркспейс dsh по умолчанию на заданном пути.
// Режим «пути как на хосте» (локальный режим): папка проектов видна в
// контейнере по тому же пути, что на машине разработчика, и воркспейс dsh
// по умолчанию должен указывать туда же — тогда пути в сессиях агента
// совпадают с хостовыми (открываются во внешнем редакторе, команды агента
// можно выполнить на хосте). Реестр — storages/workspace.json, формат
// workspace domain v2; прочие поля и записи сохраняются как есть, прежний
// воркспейс /workspace с его сессиями остаётся в списке.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnsureDefaultWorkspace делает воркспейс dsh по умолчанию воркспейсом на
// пути dir: пустой реестр засевается (как seedWorkspaceRegistry, но с этим
// путём), в существующем — находится или добавляется запись с этим путём и
// становится по умолчанию. Вызывать при остановленном dsh (до старта).
// changed=false — реестр уже такой.
func EnsureDefaultWorkspace(home, dir string) (changed bool, err error) {
	path := filepath.Join(home, "storages", "workspace.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := seedWorkspaceRegistry(home); err != nil {
			return false, err
		}
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return false, fmt.Errorf("provision: workspace.json: %w", err)
	}
	var reg map[string]any
	if err := json.Unmarshal(raw, &reg); err != nil {
		return false, fmt.Errorf("provision: workspace.json: %w", err)
	}
	global, _ := reg["global"].(map[string]any)
	tables, _ := reg["tables"].(map[string]any)
	if global == nil || tables == nil {
		return false, fmt.Errorf("provision: workspace.json: нет global/tables — формат dsh изменился")
	}
	workspaces, _ := tables["workspaces"].(map[string]any)
	if workspaces == nil {
		workspaces = map[string]any{}
		tables["workspaces"] = workspaces
	}
	id := ""
	for wid, w := range workspaces {
		if m, ok := w.(map[string]any); ok && m["path"] == dir {
			id = wid
			break
		}
	}
	if id != "" && global["defaultWorkspaceId"] == id {
		return false, nil
	}
	if id == "" {
		id = "ws-agctl-host"
		for i := 2; workspaces[id] != nil; i++ {
			id = fmt.Sprintf("ws-agctl-host-%d", i)
		}
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		workspaces[id] = map[string]any{
			"path": dir, "title": "default-workspace", "sessionIds": []any{},
			"createdAt": now, "updatedAt": now,
		}
		ids, _ := global["workspaceIds"].([]any)
		global["workspaceIds"] = append([]any{id}, ids...)
	}
	global["defaultWorkspaceId"] = id
	out, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}
