package profile

import (
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
