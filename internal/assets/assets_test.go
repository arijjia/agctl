package assets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Скелетная копия плагина кабинета обязана совпадать с исходником
// dsh-plugins/cabinet байт-в-байт (конвенция анти-дрейфа, как
// check-1c-vibe-sync). Правь исходник, затем перекопируй:
//
//	rm -rf internal/assets/dsh-skel/profiles/web/node_modules/@arijjia/agctl-cabinet
//	cp -R dsh-plugins/cabinet internal/assets/dsh-skel/profiles/web/node_modules/@arijjia/
func TestCabinetPluginSkeletonInSync(t *testing.T) {
	src := filepath.Join("..", "..", "dsh-plugins", "cabinet")
	dst := filepath.Join("dsh-skel", "profiles", "web", "node_modules", "@arijjia", "agctl-cabinet")

	files := []string{
		"package.json",
		"cordis.patch.yml",
		"README.md",
		filepath.Join("lib", "index.js"),
		filepath.Join("lib", "client.js"),
	}
	for _, f := range files {
		want, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatalf("исходник %s: %v", f, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, f))
		if err != nil {
			t.Fatalf("скелет-копия %s отсутствует: %v (перекопируй: cp -R dsh-plugins/cabinet internal/assets/dsh-skel/profiles/web/node_modules/@arijjia/)", f, err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("дрейф %s: скелет-копия отличается от dsh-plugins/cabinet — перекопируй каталог", f)
		}
	}

	// и bundles скелетного web-профиля содержат плагин последним слоем
	pj, err := os.ReadFile(filepath.Join("dsh-skel", "profiles", "web", "package.json"))
	if err != nil {
		t.Fatalf("skel web package.json: %v", err)
	}
	if !bytes.Contains(pj, []byte("@arijjia/agctl-cabinet")) {
		t.Fatal("skel web profile: bundles без @arijjia/agctl-cabinet")
	}
}
