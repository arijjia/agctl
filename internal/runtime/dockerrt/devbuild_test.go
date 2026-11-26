package dockerrt

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestDevBuildPlan(t *testing.T) {
	prof := fstest.MapFS{
		"image/Dockerfile":            {Data: []byte("FROM ${BASE}")},
		"image/flavors/1c/Dockerfile": {Data: []byte("FROM ${BASE}")},
	}
	overlay := fstest.MapFS{"Dockerfile": {Data: []byte("FROM ${BASE}")}}
	cases := []struct {
		name string
		b    DevBuild
		want []string
	}{
		{"базовый", DevBuild{Target: "agctl-dsh:0.2.2"}, []string{"agctl-dsh:0.2.2"}},
		{"минимальный", DevBuild{Target: "agctl-dsh-min:0.2.2", Flavor: "min"}, []string{"agctl-dsh-min:0.2.2"}},
		{"версия dsh тегом", DevBuild{Target: "agctl-dsh:0.2.3-alpha.2"}, []string{"agctl-dsh:0.2.3-alpha.2"}},
		{"web", DevBuild{Target: "agctl-dsh-web:0.2.2", Flavor: "web"}, []string{"agctl-dsh:0.2.2", "agctl-dsh-web:0.2.2"}},
		{"профиль", DevBuild{Target: "agctl-dsh-utmn:0.2.2", ProfileID: "utmn", Overlay: overlay, ProfileFS: prof},
			[]string{"agctl-dsh:0.2.2", "agctl-dsh-utmn:0.2.2"}},
		{"профиль+web", DevBuild{Target: "agctl-dsh-web-utmn:0.2.2", Flavor: "web", ProfileID: "utmn", Overlay: overlay, ProfileFS: prof},
			[]string{"agctl-dsh:0.2.2", "agctl-dsh-web:0.2.2", "agctl-dsh-web-utmn:0.2.2"}},
		{"профиль+1c", DevBuild{Target: "agctl-dsh-utmn-1c:0.2.2", Flavor: "1c", ProfileID: "utmn", Overlay: overlay, ProfileFS: prof},
			[]string{"agctl-dsh:0.2.2", "agctl-dsh-utmn:0.2.2", "agctl-dsh-utmn-1c:0.2.2"}},
	}
	for _, c := range cases {
		if got := DevBuildPlan(c.b); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLocalImageName(t *testing.T) {
	for ref, want := range map[string]string{
		"ghcr.io/org/agctl-dsh:0.2.2":                            "agctl-dsh:0.2.2",
		"registry.example.org/agctl/agctl-dsh-min:0.2.3-alpha.2": "agctl-dsh-min:0.2.3-alpha.2",
		"agctl-dsh-web:0.2.2":                                    "agctl-dsh-web:0.2.2",
		"ghcr.io/org/other:1":                                    "ghcr.io/org/other:1",
	} {
		if got := LocalImageName(ref); got != want {
			t.Errorf("%s → %s, нужно %s", ref, got, want)
		}
	}
}
