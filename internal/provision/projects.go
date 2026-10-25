package provision

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

var (
	ErrInvalidProject = errors.New("invalid project name or path")
	ErrProjectExists  = errors.New("project already exists")
	projectMu         sync.Mutex
)

// Project describes a real directory in a personal chat's /workspace mount.
// Directory is a container path, never a host filesystem path.
type Project struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

const projectIgnore = "# agctl: local-only project history; never save credentials\n.env\n.env.*\n.credentials\n.credentials*\n.ssh\n"

// ValidProjectName accepts a single visible path component (including Unicode).
func ValidProjectName(name string) bool {
	if name == "" || len(name) > 100 || strings.TrimSpace(name) != name || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return name != "." && name != ".." && filepath.Base(name) == name
}

// ProjectWorkRoot validates every component beneath the trusted AGCTL_HOME.
// It may provision storage before the first chat container is started.
func ProjectWorkRoot(home string, userID int64, create bool) (string, error) {
	if userID <= 0 {
		return "", ErrInvalidProject
	}
	root, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	for _, part := range []string{"chats", fmt.Sprintf("u%d", userID), "work"} {
		root = filepath.Join(root, part)
		if err := projectDirectory(root, create); err != nil {
			return "", err
		}
	}
	return root, nil
}

func projectDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && create {
		if err = os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidProject
	}
	return nil
}

// ProjectDirectory resolves an existing project for folder/session integration.
// Callers must not accept a user-supplied directory in place of the name.
func ProjectDirectory(home string, userID int64, name string) (string, error) {
	if !ValidProjectName(name) {
		return "", ErrInvalidProject
	}
	root, err := ProjectWorkRoot(home, userID, false)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, name)
	if err := projectDirectory(path, false); err != nil {
		return "", err
	}
	return path, nil
}

func projectDescriptor(name string) Project {
	return Project{Name: name, Directory: "/workspace/" + name}
}

// ListProjects lists real top-level directories, excluding hidden/session state,
// invalid names and symlinks. It never imports an arbitrary directory into git.
func ListProjects(home string, userID int64) ([]Project, error) {
	projects := []Project{}
	root, err := ProjectWorkRoot(home, userID, false)
	if os.IsNotExist(err) {
		return projects, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !ValidProjectName(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		projects = append(projects, projectDescriptor(entry.Name()))
	}
	return projects, nil
}

// CreateProject creates a directory, local repository and initial commit. No
// remote is configured. Existing directories are never overwritten or adopted.
func CreateProject(home string, userID int64, name string) (Project, error) {
	projectMu.Lock()
	defer projectMu.Unlock()
	if !ValidProjectName(name) {
		return Project{}, ErrInvalidProject
	}
	root, err := ProjectWorkRoot(home, userID, true)
	if err != nil {
		return Project{}, err
	}
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		if os.IsExist(err) {
			return Project{}, ErrProjectExists
		}
		return Project{}, err
	}
	// On failure keep the new directory for diagnosis; never recursively remove
	// paths in a volume concurrently writable by the user's container.
	ignore, err := os.OpenFile(filepath.Join(path, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Project{}, err
	}
	_, writeErr := ignore.WriteString(projectIgnore)
	closeErr := ignore.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return Project{}, err
	}
	if err := validateProjectTree(path); err != nil {
		return Project{}, err
	}
	repo, err := git.PlainInit(path, false)
	if err != nil {
		return Project{}, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return Project{}, err
	}
	if _, err := wt.Add(".gitignore"); err != nil {
		return Project{}, err
	}
	sig := signature()
	if _, err := wt.Commit("Initial project", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		return Project{}, err
	}
	return projectDescriptor(name), nil
}

// ValidCloneURL accepts only http(s) URLs with a host: cloning is a server-side
// network fetch, so other schemes (file://, ssh://, local paths) are refused.
func ValidCloneURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// CloneProject creates the project directory by cloning a remote repository.
// No credentials are used or stored — only public/pre-authorized URLs; the
// remote origin is configured by the clone itself, while autosave stays
// local-only. Existing directories are never overwritten or adopted.
func CloneProject(home string, userID int64, name, cloneURL string) (Project, error) {
	projectMu.Lock()
	defer projectMu.Unlock()
	if !ValidProjectName(name) || !ValidCloneURL(cloneURL) {
		return Project{}, ErrInvalidProject
	}
	root, err := ProjectWorkRoot(home, userID, true)
	if err != nil {
		return Project{}, err
	}
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		if os.IsExist(err) {
			return Project{}, ErrProjectExists
		}
		return Project{}, err
	}
	// On failure keep the new directory for diagnosis; never recursively remove
	// paths in a volume concurrently writable by the user's container.
	if _, err := cloneInto(path, strings.TrimSpace(cloneURL)); err != nil {
		return Project{}, err
	}
	return projectDescriptor(name), nil
}

// cloneInto performs the actual go-git clone; scheme policy lives in
// ValidCloneURL so tests can exercise the local file:// transport directly.
func cloneInto(path, cloneURL string) (Project, error) {
	if _, err := git.PlainCloneContext(context.Background(), path, false, &git.CloneOptions{
		URL: cloneURL,
	}); err != nil {
		return Project{}, err
	}
	return projectDescriptor(filepath.Base(path)), nil
}

func projectSecret(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".env" || strings.HasPrefix(part, ".env.") || strings.HasPrefix(part, ".credentials") || part == ".ssh" {
			return true
		}
	}
	return false
}

// validateProjectTree refuses links (including .git/config, objects and refs),
// special files and nested repos: git must never follow paths outside the project.
func validateProjectTree(path string) error {
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrInvalidProject
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return ErrInvalidProject
		}
		if p != path && d.Name() == ".git" && p != filepath.Join(path, ".git") {
			return ErrInvalidProject
		}
		return nil
	})
}

// AutosaveLocalProject commits locally only; it never fetches, pushes or adds
// remotes. Credential exclusions are enforced independently of .gitignore, and
// even previously staged/tracked credential files are removed from the index.
func AutosaveLocalProject(home string, userID int64, name string) (bool, error) {
	projectMu.Lock()
	defer projectMu.Unlock()
	path, err := ProjectDirectory(home, userID, name)
	if err != nil {
		return false, err
	}
	if err := projectDirectory(filepath.Join(path, ".git"), false); err != nil {
		return false, err
	}
	if err := validateProjectTree(path); err != nil {
		return false, err
	}
	repo, err := git.PlainOpen(path)
	if err != nil {
		return false, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return false, err
	}
	for _, pattern := range strings.Split(projectIgnore, "\n") {
		if pattern != "" && !strings.HasPrefix(pattern, "#") {
			wt.Excludes = append(wt.Excludes, gitignore.ParsePattern(pattern, nil))
		}
	}
	// Remove credentials directly from the index, without deleting user's files.
	idx, err := repo.Storer.Index()
	if err != nil {
		return false, err
	}
	entries := idx.Entries[:0]
	for _, entry := range idx.Entries {
		if !projectSecret(entry.Name) {
			entries = append(entries, entry)
		}
	}
	idx.Entries = entries
	if err := repo.Storer.SetIndex(idx); err != nil {
		return false, err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return false, err
	}
	status, err := wt.Status()
	if err != nil {
		return false, err
	}
	staged := false
	for _, file := range status {
		if file.Staging != git.Unmodified && file.Staging != git.Untracked {
			staged = true
			break
		}
	}
	if !staged {
		return false, nil
	}
	sig := signature()
	_, err = wt.Commit("autosave: local project", &git.CommitOptions{Author: sig, Committer: sig})
	return err == nil, err
}
