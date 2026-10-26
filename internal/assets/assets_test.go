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
		filepath.Join("test", "client.test.cjs"),
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

// The Host-side plugin registers persistent project directories as native DSH
// Workspaces. Keep source and embedded copies byte-identical for new chat homes.
func TestChatSessionFilesPluginSkeletonInSync(t *testing.T) {
	src := filepath.Join("..", "..", "dsh-plugins", "chat-session-files")
	dst := filepath.Join("dsh-skel", "profiles", "web", "node_modules", "@arijjia", "agctl-chat-session-files")
	files := []string{"package.json", "README.md", "cordis.patch.yml", filepath.Join("lib", "index.js"), filepath.Join("test", "plugin.test.js")}
	for _, f := range files {
		want, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatalf("session-files source %s: %v", f, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, f))
		if err != nil {
			t.Fatalf("session-files skeleton %s missing: %v", f, err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("session-files drift at %s; recopy dsh-plugins/chat-session-files", f)
		}
	}
	pj, err := os.ReadFile(filepath.Join("dsh-skel", "profiles", "web", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pj, []byte("@arijjia/agctl-chat-session-files")) {
		t.Fatal("skel web profile bundles lack @arijjia/agctl-chat-session-files")
	}
	patch, err := os.ReadFile(filepath.Join("dsh-skel", "profiles", "web", "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte("@arijjia/agctl-chat-session-files")) {
		t.Fatal("skel web cordis.patch.yml does not load agctl-chat-session-files Host plugin")
	}
}

// Chat-only UI plugins are shipped to chat homes dynamically (EnsureChatPlugins),
// so the dev skeleton must contain the package files but never enable it.
func TestChatBrandPluginSkeletonPresentButDisabled(t *testing.T) {
	src := filepath.Join("..", "..", "dsh-plugins", "chat-brand")
	dst := filepath.Join("dsh-skel", "profiles", "web", "node_modules", "@arijjia", "agctl-chat-brand")
	for _, f := range []string{"package.json", "cordis.patch.yml", filepath.Join("lib", "index.js"), filepath.Join("lib", "client.js"), filepath.Join("test", "client-bundle.test.js")} {
		want, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatalf("chat-brand source %s: %v", f, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, f))
		if err != nil {
			t.Fatalf("chat-brand skeleton %s missing: %v", f, err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("chat-brand drift at %s; recopy dsh-plugins/chat-brand", f)
		}
	}
	pj, err := os.ReadFile(filepath.Join("dsh-skel", "profiles", "web", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(pj, []byte("@arijjia/agctl-chat-brand")) {
		t.Fatal("skel web bundles must not enable chat-brand (dev workspaces stay clean)")
	}
	manifest, err := os.ReadFile(filepath.Join(dst, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte(`"inject": ["configForms", "locale"]`)) || !bytes.Contains(manifest, []byte(`"platform": "web"`)) {
		t.Fatal("chat-brand client must request configForms and locale services and target web platform")
	}
}
