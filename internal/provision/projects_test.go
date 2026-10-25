package provision

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestProjectNames(t *testing.T) {
	for _, name := range []string{"demo", "Мой проект", "hello-world_2.0"} {
		if !ValidProjectName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"", ".", "..", "../escape", "/tmp/escape", `x\y`, ".ssh", ".env", " a", "a ", "a\n", "a:b", "a\x00b"} {
		if ValidProjectName(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

func projectTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectCreateAndLocalAutosave(t *testing.T) {
	home := t.TempDir()
	p, err := CreateProject(home, 1, "Работа")
	if err != nil {
		t.Fatal(err)
	}
	if p.Directory != "/workspace/Работа" {
		t.Fatal(p)
	}
	path, err := ProjectDirectory(home, 1, p.Name)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if remotes, err := repo.Remotes(); err != nil || len(remotes) != 0 {
		t.Fatalf("remotes %v %v", remotes, err)
	}
	initial, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := AutosaveLocalProject(home, 1, p.Name); err != nil || changed {
		t.Fatalf("clean: %v %v", changed, err)
	}
	// Exclusions apply at every depth and remain enforced if .gitignore changes.
	projectTestWrite(t, filepath.Join(path, ".gitignore"), "# user removed exclusions, including explicit unignores\n!.env\n!.env.*\n!.credentials*\n!.ssh/\n")
	for _, secret := range []string{".env", ".env.local", ".credentials", ".credentials.yaml", ".ssh/id_ed25519", "nested/.env", "nested/.credentials/token", "nested/.ssh/config"} {
		projectTestWrite(t, filepath.Join(path, secret), "not-a-real-secret")
	}
	projectTestWrite(t, filepath.Join(path, "main.txt"), "one")
	if changed, err := AutosaveLocalProject(home, 1, p.Name); err != nil || !changed {
		t.Fatalf("save: %v %v", changed, err)
	}
	head, err := repo.Head()
	if err != nil || head.Hash() == initial.Hash() {
		t.Fatalf("head: %v %v", head, err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	files, err := commit.Files()
	if err != nil {
		t.Fatal(err)
	}
	if err := files.ForEach(func(f *object.File) error {
		if projectSecret(f.Name) {
			t.Errorf("secret committed: %s", f.Name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if changed, err := AutosaveLocalProject(home, 1, p.Name); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	// Updates and deletions are both saved.
	if err := os.Remove(filepath.Join(path, "main.txt")); err != nil {
		t.Fatal(err)
	}
	if changed, err := AutosaveLocalProject(home, 1, p.Name); err != nil || !changed {
		t.Fatalf("delete: %v %v", changed, err)
	}
	if _, err := CreateProject(home, 1, p.Name); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if list, err := ListProjects(home, 2); err != nil || len(list) != 0 {
		t.Fatalf("isolation: %v %v", list, err)
	}
}

func TestProjectAutosaveRemovesStagedCredentials(t *testing.T) {
	home := t.TempDir()
	if _, err := CreateProject(home, 1, "demo"); err != nil {
		t.Fatal(err)
	}
	path, _ := ProjectDirectory(home, 1, "demo")
	projectTestWrite(t, filepath.Join(path, ".gitignore"), "# blank\n")
	projectTestWrite(t, filepath.Join(path, ".env"), "not-a-real-secret")
	repo, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(".env"); err != nil {
		t.Fatal(err)
	}
	if _, err := AutosaveLocalProject(home, 1, "demo"); err != nil {
		t.Fatal(err)
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range idx.Entries {
		if projectSecret(entry.Name) {
			t.Fatalf("secret indexed: %s", entry.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(path, ".env")); err != nil {
		t.Fatal("secret removed from disk", err)
	}
}

func TestProjectSymlinkGuards(t *testing.T) {
	for _, component := range []string{"chats", "chats/u1", "chats/u1/work", "chats/u1/work/demo"} {
		t.Run(component, func(t *testing.T) {
			home := t.TempDir()
			target := t.TempDir()
			link := filepath.Join(home, component)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if _, err := CreateProject(home, 1, "demo"); err == nil {
				t.Fatal("created through link")
			}
			if _, err := ProjectDirectory(home, 1, "demo"); err == nil {
				t.Fatal("resolved through link")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside modified: %v %v", entries, err)
			}
		})
	}
	for _, component := range []string{"link", ".git/config", ".git/objects"} {
		t.Run("autosave-"+component, func(t *testing.T) {
			home := t.TempDir()
			if _, err := CreateProject(home, 1, "demo"); err != nil {
				t.Fatal(err)
			}
			path, _ := ProjectDirectory(home, 1, "demo")
			link := filepath.Join(path, component)
			// Only replace an exact known fixture path; never recursive-delete.
			if component == ".git/config" {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
			} else if component == ".git/objects" {
				link = filepath.Join(link, "evil")
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
			if _, err := AutosaveLocalProject(home, 1, "demo"); err == nil {
				t.Fatal("autosave accepted symlink")
			}
		})
	}
}

func TestProjectListingRealDirectories(t *testing.T) {
	home := t.TempDir()
	if _, err := CreateProject(home, 1, "real"); err != nil {
		t.Fatal(err)
	}
	root, _ := ProjectWorkRoot(home, 1, false)
	for _, name := range []string{".sessions", ".ssh", "existing"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	list, err := ListProjects(home, 1)
	if err != nil || len(list) != 2 || list[0].Name != "existing" || list[1].Name != "real" {
		t.Fatalf("list: %v %v", list, err)
	}
	if _, err := AutosaveLocalProject(home, 1, "existing"); !os.IsNotExist(err) {
		t.Fatalf("adopted existing directory: %v", err)
	}
}
