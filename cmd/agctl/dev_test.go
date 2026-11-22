package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
)

func writeDevProfile(t *testing.T, bridge string) *profile.Data {
	t.Helper()
	dir := t.TempDir()
	body := "schema: 1\nid: devorg\nbrand:\n  name: Dev Org\n  letter: D\nimages:\n  dev:\n    - agctl-dsh-devorg:0.2.2\n    - agctl-dsh-web-devorg:0.2.2\n    - agctl-dsh-devorg-1c:0.2.2\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if bridge != "" {
		_ = os.MkdirAll(filepath.Join(dir, "local"), 0o755)
		if err := os.WriteFile(filepath.Join(dir, "local", "bridge-tools.yaml"), []byte(bridge), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := profile.Resolve("", dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileBridgeTools(t *testing.T) {
	p := writeDevProfile(t, "tools:\n  - name: onec_config_check\n    description: check\n    command: [ibcmd, infobase, config, check, \"--db-path={{db}}\"]\n    args:\n      db: {required: true}\n")
	tools, err := profileBridgeTools(p)
	if err != nil || len(tools) != 1 || tools[0].Name != "onec_config_check" {
		t.Fatalf("%+v %v", tools, err)
	}
	if tools, err := profileBridgeTools(writeDevProfile(t, "")); err != nil || len(tools) != 0 {
		t.Fatal("без файла — пустой список без ошибки")
	}
	if _, err := profileBridgeTools(writeDevProfile(t, "tools:\n  - name: Bad\n    command: [x]\n")); err == nil {
		t.Fatal("невалидный белый список профиля — ошибка")
	}
}

// Реальный белый список профиля utmn (если профиль подтянут в ./profiles).
func TestUtmnBridgeToolsValid(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "profiles", "utmn", "local", "bridge-tools.yaml")); err != nil {
		t.Skip("профиль utmn не подтянут")
	}
	p, err := profile.Resolve("", filepath.Join("..", "..", "profiles", "utmn"))
	if err != nil {
		t.Fatal(err)
	}
	tools, err := profileBridgeTools(p)
	if err != nil || len(tools) == 0 {
		t.Fatalf("%v %v", tools, err)
	}
	argv, err := tools[1].Expand(map[string]string{"db": `C:\1c\ib`, "dir": `C:\src\proj\dump`})
	if err != nil || argv[len(argv)-1] != `C:\src\proj\dump` {
		t.Fatalf("%q %v", argv, err)
	}
}

func TestDevDefaultImageFlavor(t *testing.T) {
	p := writeDevProfile(t, "")
	if got := devDefaultImage(p, ""); got != "agctl-dsh-devorg:0.2.2" {
		t.Fatal(got)
	}
	if got := devDefaultImage(p, "web"); got != "agctl-dsh-web-devorg:0.2.2" {
		t.Fatal(got)
	}
	if got := devDefaultImage(p, "1c"); got != "agctl-dsh-devorg-1c:0.2.2" {
		t.Fatal(got)
	}
	neutral, _ := profile.Embedded("agctl")
	if got := devDefaultImage(neutral, "web"); got != "agctl-dsh-web:0.2.2" {
		t.Fatal(got)
	}
	_ = localmode.DefaultAddr
}

func TestDevDefaultProfile(t *testing.T) {
	mkProfile := func(t *testing.T, base, id string) {
		t.Helper()
		dir := filepath.Join(base, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte("schema: 1\nid: "+id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("нет каталога данных — нейтральный", func(t *testing.T) {
		if got := devDefaultProfile(filepath.Join(t.TempDir(), "нет-такого")); got != "agctl" {
			t.Fatalf("got %q, ждём agctl", got)
		}
	})
	t.Run("пустой каталог — нейтральный", func(t *testing.T) {
		if got := devDefaultProfile(t.TempDir()); got != "agctl" {
			t.Fatalf("got %q, ждём agctl", got)
		}
	})
	t.Run("один профиль организации — он и есть дефолт", func(t *testing.T) {
		base := t.TempDir()
		mkProfile(t, base, "utmn")
		if got := devDefaultProfile(base); got != "utmn" {
			t.Fatalf("got %q, ждём utmn", got)
		}
	})
	t.Run("несколько организаций — выбор за пользователем", func(t *testing.T) {
		base := t.TempDir()
		mkProfile(t, base, "utmn")
		mkProfile(t, base, "rshb")
		if got := devDefaultProfile(base); got != "agctl" {
			t.Fatalf("got %q, ждём agctl", got)
		}
	})
	t.Run("каталог без profile.yaml и файлы не считаются", func(t *testing.T) {
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, "мусор"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "заметка.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := devDefaultProfile(base); got != "agctl" {
			t.Fatalf("got %q, ждём agctl", got)
		}
	})
}

// TestDevResolveProfileFindsDataDir — профиль из каталога данных (куда его
// кладёт установщик комплекта) разрешается по имени без --profile-dir.
func TestDevResolveProfileFindsDataDir(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "utmn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema: 1\nid: utmn\nbrand:\n  name: Utmn\n  letter: U\nllm:\n  default_base_url: https://llm.example/v1\n  default_model: m1\n  default_models: m1,m2\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// devResolveProfile зовёт profile.DataDir() — окружение подменить нельзя,
	// поэтому проверяем сам поиск Resolve с тем же списком каталогов.
	p, err := profile.Resolve("utmn", "", "profiles", filepath.Join("нет", "profiles"), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "utmn" {
		t.Fatalf("id = %q, ждём utmn", p.ID)
	}
}

// После обновления бинаря `dev up` должен заметить, что фоновый процесс —
// старой сборки, и перезапустить его.
func TestDevStale(t *testing.T) {
	oldV, oldC := version, commit
	t.Cleanup(func() { version, commit = oldV, oldC })
	version, commit = "0.1.0-utmn.5", "163b114"
	for _, c := range []struct {
		name string
		st   localmode.State
		want bool
	}{
		{"та же сборка", localmode.State{Version: "0.1.0-utmn.5", Build: "0.1.0-utmn.5 163b114"}, false},
		{"другой коммит той же версии", localmode.State{Version: "0.1.0-utmn.5", Build: "0.1.0-utmn.5 abc1234"}, true},
		{"старое состояние без build, другая версия", localmode.State{Version: "0.1.0-utmn.3"}, true},
		{"старое состояние без build, та же версия", localmode.State{Version: "0.1.0-utmn.5"}, false},
		{"версия неизвестна", localmode.State{}, false},
	} {
		if got := devStale(&c.st); got != c.want {
			t.Errorf("%s: devStale = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDevEgressArgs — egress-allowlist из dev.yaml всегда уходит в serve
// явными флагами (и выключенный тоже): env машины его не перекрывает.
func TestDevEgressArgs(t *testing.T) {
	got := strings.Join(devEgressArgs(&localmode.Config{}), " ")
	if got != "--egress-allowlist=false --egress-allow=" {
		t.Fatalf("выключенный egress: %q", got)
	}
	got = strings.Join(devEgressArgs(&localmode.Config{EgressAllowlist: true, EgressAllow: []string{"gitlab.example.org", "10.0.0.0/8"}}), " ")
	if got != "--egress-allowlist=true --egress-allow=gitlab.example.org,10.0.0.0/8" {
		t.Fatalf("включённый egress: %q", got)
	}
}
