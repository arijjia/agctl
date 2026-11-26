package assets

import (
	"fmt"
	"strings"
	"testing"
)

// Пин dsh читается из встроенного Dockerfile, а уровень API guest в обоих
// Dockerfile совпадает с константой бинаря: иначе лаунчер сочтёт свежий
// образ устаревшим (или наоборот).
func TestImageMetaInSync(t *testing.T) {
	pin := PinnedDshVersion()
	if pin == "" || !strings.Contains(pin, ".") {
		t.Fatalf("пин dsh не найден в image/Dockerfile: %q", pin)
	}
	want := fmt.Sprintf("ARG AGCTL_GUEST_API=%d", GuestAPI)
	for _, f := range []string{"image/Dockerfile", "image/Dockerfile.min"} {
		raw, err := Image.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, want) {
			t.Errorf("%s: нет %q — поднимите вместе с assets.GuestAPI", f, want)
		}
		for _, label := range []string{LabelDshVersion, LabelGuestRevision, LabelGuestAPI, LabelVariant, LabelCreated} {
			if !strings.Contains(text, label+"=") {
				t.Errorf("%s: нет метки %s", f, label)
			}
		}
	}
	// база минимального образа дословно та же, что у полного: общий кэш слоёв
	full, _ := Image.ReadFile("image/Dockerfile")
	min, _ := Image.ReadFile("image/Dockerfile.min")
	base := func(s string) string {
		i := strings.Index(s, "FROM node:")
		j := strings.Index(s[i:], "\nFROM ")
		return s[i : i+j]
	}
	if base(string(full)) != base(string(min)) {
		t.Errorf("стадия base в Dockerfile.min разошлась с Dockerfile:\n%s\n---\n%s", base(string(full)), base(string(min)))
	}
}

func TestDshVersionFromTag(t *testing.T) {
	for tag, want := range map[string]string{
		"0.2.3-alpha.2": "0.2.3-alpha.2",
		"0.2.2-rc.5":    "0.2.2-rc.5",
		"0.2.2":         "",
		"latest":        "",
		"dev":           "",
	} {
		if got := DshVersionFromTag(tag); got != want {
			t.Errorf("%s → %q, нужно %q", tag, got, want)
		}
	}
}
