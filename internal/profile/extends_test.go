package profile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Наследование: потомок переопределяет только своё (вложенные словари
// сливаются, списки заменяются), файлы ищутся сначала у потомка, затем у
// базового; вариант внутри каталога базового находит его по id.
func TestLoadDirExtends(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"org/profile.yaml": `schema: 1
id: org
brand: {name: Орг, tagline: база, logo: assets/logo.png}
llm:
  provider_name: zai
  default_base_url: https://api.example.org/v1
  default_model: big
  default_models: big,fast
images:
  dev: [agctl-dsh:0.2.2, agctl-dsh-min:0.2.2]
env:
  JIRA_URL: https://jira.example.org
`,
		"org/assets/logo.png":                "PNG",
		"org/catalog/research/manifest.json": `{"id":"research"}`,
		"org/catalog/shared/manifest.json":   `{"id":"shared","from":"org"}`,
		"org/image/Dockerfile":               "FROM ${BASE}\n",
		"org/dev/profile.yaml": `schema: 1
id: org-dev
extends: org
llm:
  provider_name: corp
  default_base_url: https://gw.example.internal/v1
  default_model: corp-chat
  default_models: corp-chat,corp-reason
images:
  dev: [agctl-dsh:0.2.2]
env_from:
  AGCTL_LLM_API_KEY: [LLM_TOKEN]
`,
		"org/dev/catalog/shared/manifest.json":  `{"id":"shared","from":"dev"}`,
		"org/dev/catalog/gateway/manifest.json": `{"id":"gateway"}`,
		"vpn/profile.yaml": `schema: 1
id: org-vpn
extends: ../org
brand: {tagline: VPN}
env:
  CONFLUENCE_URL: https://wiki.example.org
`,
	})
	d, err := LoadDir(filepath.Join(root, "org", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	if v.ID != "org-dev" || v.Extends != "org" || v.Name != "Орг" || v.Tagline != "база" {
		t.Fatalf("бренд и id: %+v", v)
	}
	if v.LLMProvider != "corp" || v.LLMModel != "corp-chat" || v.LLMBaseURL != "https://gw.example.internal/v1" {
		t.Fatalf("llm потомка: %+v", v)
	}
	if len(v.DevImages) != 1 {
		t.Fatalf("списки заменяются целиком: %v", v.DevImages)
	}
	if v.Env["JIRA_URL"] != "https://jira.example.org" || v.EnvFrom["AGCTL_LLM_API_KEY"][0] != "LLM_TOKEN" {
		t.Fatalf("env базового и env_from потомка: %v %v", v.Env, v.EnvFrom)
	}
	if v.LogoDataURI == "" {
		t.Fatal("логотип базового профиля не найден")
	}
	ents, err := fs.ReadDir(v.FS, "catalog")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "gateway,research,shared" {
		t.Fatalf("каталог — объединение слоёв: %v", names)
	}
	raw, err := fs.ReadFile(v.FS, "catalog/shared/manifest.json")
	if err != nil || !strings.Contains(string(raw), `"from":"dev"`) {
		t.Fatalf("файл потомка закрывает базовый: %s %v", raw, err)
	}
	if _, err := fs.Stat(v.FS, "image/Dockerfile"); err != nil {
		t.Fatalf("образ базового профиля: %v", err)
	}
	if ctx, file, ok := v.ImageContext(); ok || ctx != nil || file != "" {
		t.Fatal("images.dockerfile не задан — надстройки нет")
	}

	// путь вместо id; словарь brand сливается, а не заменяется
	vpn, err := LoadDir(filepath.Join(root, "vpn"))
	if err != nil {
		t.Fatal(err)
	}
	if vv := vpn.View(); vv.Name != "Орг" || vv.Tagline != "VPN" || vv.Env["JIRA_URL"] == "" || vv.Env["CONFLUENCE_URL"] == "" {
		t.Fatalf("vpn: %+v env=%v", vv, vv.Env)
	}

	// CheckSource (редактор профиля) видит базовый
	if _, err := CheckSource(filepath.Join(root, "org", "dev"), "org-dev", []byte("schema: 1\nid: org-dev\nextends: org\n")); err != nil {
		t.Fatalf("CheckSource с extends: %v", err)
	}
}

func TestLoadDirExtendsErrors(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"a/profile.yaml":       "schema: 1\nid: a\nextends: b\nbrand: {name: A}\n",
		"b/profile.yaml":       "schema: 1\nid: b\nextends: a\nbrand: {name: B}\n",
		"lost/profile.yaml":    "schema: 1\nid: lost\nextends: nowhere\nbrand: {name: L}\n",
		"secret/profile.yaml":  "schema: 1\nid: secret\nbrand: {name: S}\nenv:\n  JIRA_API_TOKEN: x\n",
		"badfrom/profile.yaml": "schema: 1\nid: badfrom\nbrand: {name: S}\nenv_from:\n  AGCTL_LLM_API_KEY: [llm-token]\n",
	})
	for dir, want := range map[string]string{
		"a": "цикл", "lost": "не найден", "secret": "похоже на секрет", "badfrom": "имя переменной хоста",
	} {
		if _, err := LoadDir(filepath.Join(root, dir)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: ждали %q, получили %v", dir, want, err)
		}
	}
}
