package provision

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

// Профиль без catalog/ (или без файловой части) — пустой каталог, не ошибка.
func TestCatalogPackagesWithoutCatalog(t *testing.T) {
	for name, fsys := range map[string]fs.FS{
		"nil FS":       nil,
		"нет catalog/": fstest.MapFS{"profile.yaml": {Data: []byte("schema: 1")}},
	} {
		pkgs, err := CatalogPackages(fsys)
		if err != nil || len(pkgs) != 0 {
			t.Fatalf("%s: %v %+v", name, err, pkgs)
		}
	}
	if _, err := FindCatalogPackage(nil, "presentations"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nil FS: %v", err)
	}
}
