package localmode

import "testing"

func TestRuntimeChoice(t *testing.T) {
	for in, want := range map[string]string{"": RuntimeDocker, "docker": RuntimeDocker, " SBX ": RuntimeSbx} {
		if got, err := ParseRuntime(in); err != nil || got != want {
			t.Errorf("ParseRuntime(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseRuntime("lxc"); err == nil {
		t.Error("неизвестный рантайм должен отклоняться")
	}
	home := t.TempDir()
	if err := (&Config{Image: "img:1"}).Save(home); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load(home)
	if cfg.RuntimeKind() != RuntimeDocker {
		t.Fatalf("по умолчанию docker, а не %q", cfg.RuntimeKind())
	}
	if err := SetRuntime(home, "sbx"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = Load(home); cfg.RuntimeKind() != RuntimeSbx || cfg.Image != "img:1" {
		t.Fatalf("после SetRuntime: %+v", cfg)
	}
	if err := SetRuntime(home, "lxc"); err == nil {
		t.Fatal("SetRuntime должен отклонять неизвестное значение")
	}
}
