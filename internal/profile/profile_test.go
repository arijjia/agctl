package profile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedNeutralIsOrgFree(t *testing.T) {
	d, err := Resolve("agctl", "")
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	if v.Name != "agctl" || v.Title != "agctl" || v.Letter != "a" {
		t.Fatalf("нейтральный профиль испорчен: %+v", v)
	}
	if v.LogoDataURI != "" {
		t.Fatal("нейтральный профиль не должен нести логотип организации")
	}
	if v.LLMBaseURL != "" || v.LLMModel != "" || v.LLMModels != "" {
		t.Fatalf("нейтральный профиль не должен навязывать LLM: %+v", v)
	}
	for _, org := range []string{"utmn", "ТюмГУ", "Тюмень"} {
		if strings.Contains(v.BrandJSON(), org) {
			t.Fatalf("нейтральный бренд-JSON содержит %q: %s", org, v.BrandJSON())
		}
	}
}

// testLogoPNG — валидный PNG 1×1 для тестов логотипа.
var testLogoPNG = []byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R',
	0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89,
	0, 0, 0, 13, 'I', 'D', 'A', 'T', 'x', 0x9a, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0,
	5, 1, 2, 0, 0x49, 0x2d, 0x1d, 0x86, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// writeFixtureProfile пишет синтетический профиль организации в каталог
// (ядро не носит данных реальных организаций — они в отдельном репо).
func writeFixtureProfile(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	body := "schema: 1\n" +
		"id: " + name + "\n" +
		"brand:\n" +
		"  name: Fixture Org\n" +
		"  tagline: fixture tagline\n" +
		"  letter: F\n" +
		"  logo: assets/logo.png\n" +
		"login:\n" +
		"  subtitle: fixture subtitle\n" +
		"llm:\n" +
		"  default_base_url: https://llm.fixture.example/v1\n" +
		"  default_model: fixture-fast\n" +
		"  default_models: fixture-fast,fixture-deep\n" +
		"help:\n" +
		"  cabinet_title: Справка — Fixture Org\n"
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.png"), testLogoPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEmbeddedProfilesOnlyNeutral(t *testing.T) {
	names := EmbeddedNames()
	if len(names) != 1 || names[0] != "agctl" {
		t.Fatalf("в ядре должен быть встроен только нейтральный agctl, есть: %v", names)
	}
	if _, err := Embedded("agctl"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveFindsDiskProfileInSearchDirs(t *testing.T) {
	// make profile-fetch кладёт профиль в ./profiles/<имя>; Resolve должен
	// находить его по имени через search-каталоги.
	root := t.TempDir()
	dir := writeFixtureProfile(t, "fixture-org")
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(root, "profiles", "fixture-org")); err != nil {
		t.Fatal(err)
	}
	d, err := Resolve("fixture-org", "", filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "fixture-org" || d.Source != "dir:"+filepath.Join(root, "profiles", "fixture-org") {
		t.Fatalf("дисковый профиль не разрешён: id=%s source=%s", d.ID, d.Source)
	}
	v := d.View()
	if !strings.HasPrefix(v.LogoDataURI, "data:image/png;base64,") {
		t.Fatal("логотип фикстуры не закодирован")
	}
	if v.LLMBaseURL != "https://llm.fixture.example/v1" {
		t.Fatalf("LLM-дефолт дискового профиля: %q", v.LLMBaseURL)
	}
}

func TestResolveUnknownNameListsEmbedded(t *testing.T) {
	_, err := Resolve("no-such-profile", "")
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестного профиля")
	}
	if !strings.Contains(err.Error(), "agctl") || !strings.Contains(err.Error(), "profile-dir") {
		t.Fatalf("ошибка не подсказывает варианты: %v", err)
	}
}

func TestLoadDirWithLogoAndDefaults(t *testing.T) {
	dir := t.TempDir()
	logo := []byte("PNGDATA-not-really")
	yamlBody := `schema: 1
id: exorg
brand:
  name: Example Org
  tagline: be example
  logo: assets/logo.png
llm:
  default_base_url: https://llm.example.org/v1
`
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.png"), logo, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	// Дефолты: title = name, letter = первая буква имени.
	if v.Title != "Example Org" || v.Letter != "E" {
		t.Fatalf("дефолты title/letter: %+v", v)
	}
	if !strings.HasPrefix(v.LogoDataURI, "data:image/png;base64,") || v.LogoDataURI == "data:image/png;base64," {
		t.Fatal("логотип не закодирован в data URI")
	}
	if v.LLMBaseURL != "https://llm.example.org/v1" {
		t.Fatalf("LLM-дефолт дискового профиля: %q", v.LLMBaseURL)
	}
	if d.Source != "dir:"+dir {
		t.Fatalf("source: %q", d.Source)
	}
}

func TestLoadDirProviders(t *testing.T) {
	dir := t.TempDir()
	body := `schema: 1
id: multi
tax:
  name: x
brand:
  name: Multi Org
llm:
  provider_name: zai-coding-cn
  default_base_url: https://zai.example/v1
  default_model: glm-fast
  default_models: glm-fast,glm-deep
  extra_providers:
    - name: corp-litellm
      base_url: https://litellm.example/v1
      model: qwen-fast
      models: qwen-fast,qwen-deep
      api_key_env: AGCTL_LLM_CORP_API_KEY
`
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	if v.LLMProvider != "zai-coding-cn" {
		t.Fatalf("provider_name = %q", v.LLMProvider)
	}
	if len(v.LLMExtraProviders) != 1 {
		t.Fatalf("extra providers = %d, want 1", len(v.LLMExtraProviders))
	}
	extra := v.LLMExtraProviders[0]
	if extra.Name != "corp-litellm" || extra.BaseURL != "https://litellm.example/v1" || extra.APIKeyEnv != "AGCTL_LLM_CORP_API_KEY" {
		t.Fatalf("extra provider = %+v", extra)
	}
}

func TestResolveDirOverridesName(t *testing.T) {
	dir := t.TempDir()
	body := "schema: 1\nid: diskdir\nbrand:\n  name: Disk Org\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Resolve("utmn", dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "diskdir" {
		t.Fatalf("--profile-dir должен побеждать --profile: id=%s", d.ID)
	}
}

func TestBrandJSONEscapesHTML(t *testing.T) {
	dir := t.TempDir()
	body := "schema: 1\nid: xss\nbrand:\n  name: Bad <script>alert(1)</script> Org\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	js := d.View().BrandJSON()
	if strings.Contains(js, "<script>") {
		t.Fatalf("brand JSON не экранирует HTML: %s", js)
	}
	if !strings.Contains(js, "\\u003c") {
		t.Fatalf("ожидалась \\u003c-экранизация: %s", js)
	}
}

func TestBrandBootScriptShape(t *testing.T) {
	v := Neutral()
	script := v.BrandBootScript()
	if !strings.HasPrefix(script, `<script data-agctl="brand-boot">window.__AGCTL_BRAND__={`) {
		t.Fatalf("boot-скрипт потерял форму: %q", script)
	}
	if !strings.HasSuffix(script, ";</script>") {
		t.Fatalf("boot-скрипт не закрыт: %q", script)
	}
}

func TestOverlayImage(t *testing.T) {
	cases := map[string]string{
		"agctl-dsh:0.2.2":           "agctl-dsh-utmn:0.2.2",
		"agctl-dsh-nossh:0.2.2":     "agctl-dsh-nossh-utmn:0.2.2",
		"reg:5000/team/agctl-dsh:1": "reg:5000/team/agctl-dsh-utmn:1",
		"reg:5000/agctl-dsh":        "reg:5000/agctl-dsh-utmn",
		"agctl-dsh":                 "agctl-dsh-utmn",
	}
	for in, want := range cases {
		if got := OverlayImage(in, "utmn"); got != want {
			t.Errorf("OverlayImage(%q) = %q, want %q", in, got, want)
		}
	}
	if got := OverlayImage("agctl-dsh:1", ""); got != "agctl-dsh:1" {
		t.Errorf("пустой id: %q", got)
	}
}

func TestProfileWorkspaceAndImages(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("profile.yaml", `schema: 1
id: org
brand:
  name: Org
workspace:
  scenarios:
    - id: s1
      title: Сценарий
      clone_url: https://example.com/s1.git
images:
  dockerfile: image/Dockerfile
  dev: [agctl-dsh-org:1, agctl-dsh-nossh-org:1]
`)
	write("image/Dockerfile", "ARG BASE\nFROM ${BASE}\n")
	write("catalog/pkg/manifest.json", `{"id":"pkg"}`)
	d, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	if len(v.Scenarios) != 1 || v.Scenarios[0].CloneURL != "https://example.com/s1.git" {
		t.Fatalf("scenarios: %+v", v.Scenarios)
	}
	if len(v.DevImages) != 2 || v.DevImages[1] != "agctl-dsh-nossh-org:1" {
		t.Fatalf("dev images: %+v", v.DevImages)
	}
	ctx, file, ok := v.ImageContext()
	if !ok || file != "Dockerfile" {
		t.Fatalf("image context: ok=%v file=%q", ok, file)
	}
	if _, err := fs.Stat(ctx, "Dockerfile"); err != nil {
		t.Fatalf("Dockerfile в контексте: %v", err)
	}
	if _, err := fs.Stat(v.FS, "catalog/pkg/manifest.json"); err != nil {
		t.Fatalf("catalog в FS профиля: %v", err)
	}

	// нейтральный профиль: ни сценариев, ни надстройки образа
	n := Neutral()
	if len(n.Scenarios) != 0 {
		t.Fatalf("нейтральный профиль несёт сценарии: %+v", n.Scenarios)
	}
	if _, _, ok := n.ImageContext(); ok {
		t.Fatal("нейтральный профиль с надстройкой образа")
	}
}

func TestProfileRejectsBadWorkspace(t *testing.T) {
	for name, body := range map[string]string{
		"сценарий без clone_url": "workspace:\n  scenarios:\n    - id: a\n      title: A\n",
		"повтор id":              "workspace:\n  scenarios:\n    - {id: a, title: A, clone_url: u}\n    - {id: a, title: B, clone_url: u}\n",
		"dockerfile вне профиля": "images:\n  dockerfile: ../x/Dockerfile\n",
	} {
		dir := t.TempDir()
		raw := "schema: 1\nid: org\nbrand:\n  name: Org\n" + body
		if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadDir(dir); err == nil {
			t.Errorf("%s: ждём ошибку", name)
		}
	}
}
