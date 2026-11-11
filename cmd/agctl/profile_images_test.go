package main

import (
	"flag"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

func TestProfileImages(t *testing.T) {
	org := &profile.Data{ID: "utmn"}
	org.Images.Dockerfile = "image/Dockerfile"
	plain := &profile.Data{ID: "rshb"}

	run := func(prof *profile.Data, args ...string) (string, string) {
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		image := fs.String("image", "agctl-dsh:0.2.2", "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return profileImages(fs, prof, *image)
	}
	if b, w := run(org); b != "agctl-dsh:0.2.2" || w != "agctl-dsh-utmn:0.2.2" {
		t.Fatalf("надстройка профиля: base=%s work=%s", b, w)
	}
	if b, w := run(plain); b != w || w != "agctl-dsh:0.2.2" {
		t.Fatalf("профиль без надстройки: base=%s work=%s", b, w)
	}
	if b, w := run(org, "--image", "my:1"); b != "my:1" || w != "my:1" {
		t.Fatalf("явный --image главнее профиля: base=%s work=%s", b, w)
	}
}
