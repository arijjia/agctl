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

func TestEmbeddedUtmnMatchesExtractedDefaults(t *testing.T) {
	d, err := Embedded("utmn")
	if err != nil {
		t.Fatal(err)
	}
	v := d.View()
	if v.Name != "ИИ-мастерская ТюмГУ" || v.Tagline != "Тюмень на правильном пути" {
		t.Fatalf("бренд utmn потерян: %+v", v)
	}
	if v.LLMBaseURL != "https://litellm-dev.utmn.ru/v1" || v.LLMModel != "qwen-3.8-fast" {
		t.Fatalf("LLM-дефолты utmn потеряны: %+v", v)
	}
	if !strings.HasPrefix(v.LogoDataURI, "data:image/png;base64,iVBORw0KGgo") {
		t.Fatal("логотип utmn отсутствует или не PNG")
	}
	if v.HelpCabinetTitle != "Справка — ИИ-мастерская ТюмГУ" {
		t.Fatalf("заголовок справки utmn потерян: %q", v.HelpCabinetTitle)
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

func TestResolveUnknownNameListsEmbedded(t *testing.T) {
	_, err := Resolve("no-such-profile", "")
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестного профиля")
	}
	if !strings.Contains(err.Error(), "agctl") || !strings.Contains(err.Error(), "profile-dir") {
		t.Fatalf("ошибка не подсказывает варианты: %v", err)
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
