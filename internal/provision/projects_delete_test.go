package provision

// projects_delete_test.go — удаление воркспейса с диска (DELETE
// /api/projects) и адоптация пустого каталога при создании.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteProjectRemovesDirectory(t *testing.T) {
	home := t.TempDir()
	if _, err := CreateProject(home, 7, "deleteme"); err != nil {
		t.Fatal(err)
	}
	dir, err := ProjectDirectory(home, 7, "deleteme")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProject(home, 7, "deleteme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("каталог не удалён: %v", err)
	}
	// повторное удаление — «уже нет»
	if err := DeleteProject(home, 7, "deleteme"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("re-delete: %v, ждём os.ErrNotExist", err)
	}
	// чужой/несуществующий пользователь — просто «нет»
	if err := DeleteProject(home, 8, "deleteme"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("other user: %v", err)
	}
	// мусорное имя
	if err := DeleteProject(home, 7, "../escape"); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("escape: %v", err)
	}
}

func TestDeleteProjectRefusesNonDirectory(t *testing.T) {
	home := t.TempDir()
	root, err := ProjectWorkRoot(home, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	// имя "stray" валидно, но это файл — отказ, файл не тронут
	if err := DeleteProject(home, 3, "stray"); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delete file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "stray")); err != nil {
		t.Fatalf("файл удалён: %v", err)
	}
}

func TestCreateProjectAdoptsEmptyDirectory(t *testing.T) {
	home := t.TempDir()
	root, err := ProjectWorkRoot(home, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(root, "adopted")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProject(home, 5, "adopted"); err != nil {
		t.Fatalf("adopt empty: %v", err)
	}
	// теперь это полноценный проект: git и .gitignore на месте
	if _, err := os.Stat(filepath.Join(empty, ".git")); err != nil {
		t.Fatalf("git не инициализирован: %v", err)
	}
	// непустой каталог — прежнее поведение «уже существует»
	full := filepath.Join(root, "full")
	if err := os.Mkdir(full, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "data.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProject(home, 5, "full"); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("non-empty: %v, ждём ErrProjectExists", err)
	}
	if _, err := os.Stat(filepath.Join(full, "data.txt")); err != nil {
		t.Fatalf("данные чужого каталога тронуты: %v", err)
	}
}
