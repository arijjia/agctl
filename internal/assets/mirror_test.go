package assets

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Гард дрейфа зеркал клиентских плагинов (карточка dsh-plugin-mirror-guard).
// Исходники плагинов — dsh-plugins/<name>/, встроенная копия, которую раздаёт
// EnsureChatPlugins, — dsh-skel/profiles/web/node_modules/@arijjia/agctl-<name>/.
// Правка только одного зеркала не доходит до работающего чата, поэтому оба
// дерева обязаны совпадать байт в байт (node_modules внутри плагина не в счёт).

const (
	pluginSrcDir    = "../../dsh-plugins"
	pluginMirrorDir = "dsh-skel/profiles/web/node_modules/@arijjia"
	pluginPrefix    = "agctl-"
)

func TestDshPluginMirrorsInSync(t *testing.T) {
	src := listDirs(t, pluginSrcDir, "")
	mirror := listDirs(t, pluginMirrorDir, pluginPrefix)
	if len(src) == 0 {
		t.Fatalf("в %s нет плагинов — путь гарда устарел", pluginSrcDir)
	}
	for name := range src {
		if !mirror[name] {
			t.Errorf("плагин %s без встроенной копии; синхронизация: %s", name, syncHint(name))
		}
	}
	for name := range mirror {
		if !src[name] {
			t.Errorf("встроенная копия %s%s без исходников в dsh-plugins/%s", pluginPrefix, name, name)
		}
	}
	for name := range src {
		if !mirror[name] {
			continue
		}
		if diffs := diffTrees(t, filepath.Join(pluginSrcDir, name), filepath.Join(pluginMirrorDir, pluginPrefix+name)); len(diffs) > 0 {
			t.Errorf("зеркала плагина %s разошлись:\n  %s\nсинхронизация (исходник — dsh-plugins): %s",
				name, strings.Join(diffs, "\n  "), syncHint(name))
		}
	}
}

func syncHint(name string) string {
	return "rm -rf internal/assets/" + pluginMirrorDir + "/" + pluginPrefix + name +
		" && cp -R dsh-plugins/" + name + " internal/assets/" + pluginMirrorDir + "/" + pluginPrefix + name
}

// listDirs — имена подкаталогов dir с префиксом prefix (префикс срезан).
func listDirs(t *testing.T, dir, prefix string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение %s: %v", dir, err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			out[strings.TrimPrefix(e.Name(), prefix)] = true
		}
	}
	return out
}

// diffTrees — расхождения двух деревьев: файлы только в одном и разное содержимое.
func diffTrees(t *testing.T, a, b string) []string {
	t.Helper()
	fa, fb := treeFiles(t, a), treeFiles(t, b)
	var diffs []string
	for rel, da := range fa {
		db, ok := fb[rel]
		switch {
		case !ok:
			diffs = append(diffs, "только в dsh-plugins: "+rel)
		case !bytes.Equal(da, db):
			diffs = append(diffs, "различается: "+rel)
		}
	}
	for rel := range fb {
		if _, ok := fa[rel]; !ok {
			diffs = append(diffs, "только во встроенной копии: "+rel)
		}
	}
	sort.Strings(diffs)
	return diffs
}

func treeFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", root, err)
	}
	return out
}
