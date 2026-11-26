package provision

// preset_patch.go — управляемые пресеты agctl в формате текущего dsh.
//
// dsh (с 0.2.2-rc) больше не читает каталоги $DSH_HOME/.agent-presets/<id>/:
// пресет — это строка-декларация плагина @deepseek-ai/dsh-agent-preset в
// патче профиля, и её config.plugins — ПОЛНЫЙ состав агента, а не добавка к
// штатному. Поэтому agctl держит на диске прежний источник истины
// (.agent-presets/agctl-<id>/: preset.yml, agent.cordis.yml, skills/), а
// перед стартом dsh guest собирает из него патч-слой:
//
//	состав пресета = состав штатного пресета standard этой версии dsh
//	               + MCP-ряды пресета (agent.cordis.yml)
//	               + каталог скиллов пресета (skills/) в skill-filesystem
//
// Патч пишется в $DSH_HOME/.agctl/presets.patch.yml и подключается
// `dsh web --patch` (entrypoint образа). Состав standard берётся из
// установленного dsh в контейнере — версии не расходятся.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// ManagedPresetPrefix — марка управляемого пресета в имени каталога.
	ManagedPresetPrefix = "agctl-"
	// PresetPatchRel — путь собранного патч-слоя внутри DSH_HOME.
	PresetPatchRel = ".agctl/presets.patch.yml"
	// presetSkillsDir — каталог скиллов внутри каталога пресета.
	presetSkillsDir = "skills"
	// presetOrderBase — позиция первого управляемого пресета в ростере dsh
	// (штатные standard/ptc/minimal/cordis занимают 1..4).
	presetOrderBase = 10

	agentPresetPlugin   = "@deepseek-ai/dsh-agent-preset"
	skillFilesystemName = "@deepseek-ai/dsh-skill-filesystem"
	standardPresetRowID = "preset-standard"
)

// presetMeta — preset.yml управляемого пресета (имя и описание в ростере).
type presetMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// PresetSkillsRoot — каталог скиллов управляемого пресета в dsh-home.
func PresetSkillsRoot(dshHome, presetID string) string {
	return filepath.Join(dshHome, ".agent-presets", presetID, presetSkillsDir)
}

// StandardPresetPlugins достаёт состав штатного пресета standard из
// presets/standard.patch.yml бандла dsh-web-app. Узлы возвращаются как есть:
// теги `!!js` и комментарии сохраняются при обратной записи.
func StandardPresetPlugins(standardPatch []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(standardPatch, &doc); err != nil {
		return nil, fmt.Errorf("штатный пресет: разбор: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil, errors.New("штатный пресет: ожидался список патчей")
	}
	for _, patch := range doc.Content[0].Content {
		rows := mapValue(patch, "insert")
		if rows == nil || rows.Kind != yaml.SequenceNode {
			continue
		}
		for _, row := range rows.Content {
			if id := mapValue(row, "id"); id == nil || id.Value != standardPresetRowID {
				continue
			}
			plugins := mapValue(mapValue(row, "config"), "plugins")
			if plugins == nil || plugins.Kind != yaml.SequenceNode {
				return nil, errors.New("штатный пресет: у preset-standard нет config.plugins")
			}
			return plugins, nil
		}
	}
	return nil, errors.New("штатный пресет: строка preset-standard не найдена")
}

// BuildPresetPatch собирает патч-слой dsh из управляемых пресетов dsh-home.
// basePlugins — состав штатного пресета (StandardPresetPlugins); skillsMount —
// путь DSH_HOME, каким его видит dsh (в контейнере /home/dsh), для каталогов
// скиллов. Возвращает YAML патча и число пресетов; пресетов нет — (nil, 0).
func BuildPresetPatch(dshHome, skillsMount string, basePlugins *yaml.Node) ([]byte, int, error) {
	root := filepath.Join(dshHome, ".agent-presets")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("пресеты: %w", err)
	}
	ids := []string{}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ManagedPresetPrefix) {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	rows := &yaml.Node{Kind: yaml.SequenceNode}
	for i, id := range ids {
		dir := filepath.Join(root, id)
		meta := presetMeta{Name: id}
		if raw, err := os.ReadFile(filepath.Join(dir, "preset.yml")); err == nil { // #nosec G304
			// Файлы прежних сборок agctl писались без кавычек и могут не
			// разбираться — тогда пресет остаётся с именем-идентификатором.
			_ = yaml.Unmarshal(raw, &meta)
			if strings.TrimSpace(meta.Name) == "" {
				meta.Name = id
			}
		}
		plugins := cloneNode(basePlugins)
		if raw, err := os.ReadFile(filepath.Join(dir, "agent.cordis.yml")); err == nil { // #nosec G304
			var mcp yaml.Node
			if err := yaml.Unmarshal(raw, &mcp); err != nil {
				return nil, 0, fmt.Errorf("пресет %s: agent.cordis.yml: %w", id, err)
			}
			if len(mcp.Content) > 0 && mcp.Content[0].Kind == yaml.SequenceNode {
				plugins.Content = append(plugins.Content, mcp.Content[0].Content...)
			}
		}
		if fi, err := os.Stat(filepath.Join(dir, presetSkillsDir)); err == nil && fi.IsDir() {
			addPresetSkillsDir(plugins, skillsMount+"/.agent-presets/"+id+"/"+presetSkillsDir)
		}
		cfg := &yaml.Node{Kind: yaml.MappingNode}
		putScalar(cfg, "id", id)
		putScalar(cfg, "name", meta.Name)
		if meta.Description != "" {
			putScalar(cfg, "description", meta.Description)
		}
		cfg.Content = append(cfg.Content, scalar("order"),
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(presetOrderBase + i)})
		cfg.Content = append(cfg.Content, scalar("plugins"), plugins)
		row := &yaml.Node{Kind: yaml.MappingNode}
		putScalar(row, "id", "preset-"+id)
		putScalar(row, "name", agentPresetPlugin)
		row.Content = append(row.Content, scalar("config"), cfg)
		rows.Content = append(rows.Content, row)
	}
	if len(rows.Content) == 0 {
		return nil, 0, nil
	}
	insert := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("insert"), rows}}
	doc := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{insert}}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, 0, fmt.Errorf("пресеты: сборка патча: %w", err)
	}
	header := "# agctl: пресеты лаунчера и воркспейсов. Файл пересобирается при каждом старте\n" +
		"# контейнера из .agent-presets/agctl-*/ — вручную не редактируйте.\n"
	return append([]byte(header), out...), len(rows.Content), nil
}

// SyncPresetPatch пересобирает $DSH_HOME/.agctl/presets.patch.yml. Пресетов
// нет — файл удаляется. Возвращает путь файла ("" — патча нет) и число пресетов.
func SyncPresetPatch(dshHome, skillsMount string, standardPatch []byte) (string, int, error) {
	target := filepath.Join(dshHome, filepath.FromSlash(PresetPatchRel))
	base, err := StandardPresetPlugins(standardPatch)
	if err != nil {
		return "", 0, err
	}
	out, n, err := BuildPresetPatch(dshHome, skillsMount, base)
	if err != nil {
		return "", 0, err
	}
	if n == 0 {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return "", 0, err
		}
		return "", 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", 0, err
	}
	// 0600: в MCP-рядах лежат значения доступов (как в agent.cordis.yml).
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", 0, err
	}
	return target, n, nil
}

// addPresetSkillsDir дописывает каталог скиллов пресета в customSkillDirs
// строки skill-filesystem; строки нет — добавляет свою.
func addPresetSkillsDir(plugins *yaml.Node, dir string) {
	for _, row := range plugins.Content {
		if name := mapValue(row, "name"); name == nil || name.Value != skillFilesystemName {
			continue
		}
		cfg := mapValue(row, "config")
		if cfg == nil {
			cfg = &yaml.Node{Kind: yaml.MappingNode}
			row.Content = append(row.Content, scalar("config"), cfg)
		}
		dirs := mapValue(cfg, "customSkillDirs")
		if dirs == nil || dirs.Kind != yaml.SequenceNode {
			dirs = &yaml.Node{Kind: yaml.SequenceNode}
			cfg.Content = append(cfg.Content, scalar("customSkillDirs"), dirs)
		}
		dirs.Content = append(dirs.Content, scalar(dir))
		return
	}
	cfg := &yaml.Node{Kind: yaml.MappingNode}
	cfg.Content = append(cfg.Content, scalar("customSkillDirs"),
		&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{scalar(dir)}})
	row := &yaml.Node{Kind: yaml.MappingNode}
	putScalar(row, "id", "agctl-preset-skills")
	putScalar(row, "name", skillFilesystemName)
	row.Content = append(row.Content, scalar("config"), cfg)
	plugins.Content = append(plugins.Content, row)
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func putScalar(m *yaml.Node, key, value string) {
	m.Content = append(m.Content, scalar(key), scalar(value))
}

// mapValue — значение ключа YAML-отображения; nil, если узел не отображение
// или ключа нет.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// cloneNode — глубокая копия узла: состав штатного пресета общий для всех
// управляемых, а каждый дописывает в него своё.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}
