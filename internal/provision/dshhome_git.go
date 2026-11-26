package provision

// dshhome_git.go — dsh-home рабочего места как git-репозиторий конфигурации
// агента (локальный режим, явная папка dsh-home): скиллы, пресеты, плагины
// и настройки версионируются и переносятся на другие места и машины
// обычным git push/pull. Секреты, сессии, кэши и файлы, которые agctl
// пересобирает на каждом старте, в git не попадают (.gitignore ниже).
// Нужен git на хосте; без него папка работает как обычный dsh-home.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DshHomeGitignore — что из dsh-home не идёт в git.
const DshHomeGitignore = `# agctl: dsh-home как git-репозиторий конфигурации агента — скиллы,
# пресеты, плагины (package.json), настройки. Секреты, сессии, кэши и то,
# что agctl пересобирает на каждом старте, — вне git.

# секреты: ключи моделей, доступы, токен моста
.credentials.yaml
.credentials.yaml.*
.agctl/
.anonymous-user-id

# пресеты лаунчера agctl (пересобираются, в env MCP — доступы этой машины)
.agent-presets/agctl-*/

# сессии и рабочее состояние dsh (перенос сессий — отдельно)
sessions/
storages/

# зависимости и мусор
node_modules/
*.log
.DS_Store
`

// ErrNoGit — на хосте нет git: папка работает без версионирования.
var ErrNoGit = errors.New("git не найден на этой машине")

func gitRun(dir string, args ...string) (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", ErrNoGit
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// DshHomeRepo — состояние git-репозитория dsh-home.
type DshHomeRepo struct {
	Repo    bool   `json:"repo"`             // в папке есть свой .git
	Commits bool   `json:"commits"`          // есть хотя бы один коммит
	Remote  string `json:"remote,omitempty"` // адрес origin ("" — не настроен)
	Dirty   bool   `json:"dirty"`            // есть незакоммиченные изменения
	Branch  string `json:"branch,omitempty"`
}

// DshHomeRepoState — что с git в папке dsh-home (без git — Repo=false).
func DshHomeRepoState(dir string) DshHomeRepo {
	var st DshHomeRepo
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return st
	}
	st.Repo = true
	if _, err := gitRun(dir, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		st.Commits = true
	}
	st.Remote, _ = gitRun(dir, "remote", "get-url", "origin")
	st.Branch, _ = gitRun(dir, "symbolic-ref", "--short", "-q", "HEAD")
	if out, err := gitRun(dir, "status", "--porcelain"); err == nil && out != "" {
		st.Dirty = true
	}
	return st
}

// InitDshHomeRepo — сделать папку dsh-home git-репозиторием: git init
// (ветка main) и .gitignore. Уже репозиторий — только дописывает
// .gitignore, если его нет. created=true — репозиторий создан сейчас.
func InitDshHomeRepo(dir string) (created bool, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if _, err := gitRun(dir, "init", "-q"); err != nil {
			return false, err
		}
		// ветка main и без init.defaultBranch в конфиге хоста
		if _, err := gitRun(dir, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
			return false, err
		}
		created = true
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); os.IsNotExist(err) {
		if err := os.WriteFile(ignore, []byte(DshHomeGitignore), 0o644); err != nil {
			return created, err
		}
	}
	return created, nil
}

// CommitDshHome — закоммитить всё, что не исключено .gitignore. Пустой
// набор изменений — committed=false без ошибки. Автор — из git config
// хоста; если его нет, коммитит agctl.
func CommitDshHome(dir, message string) (committed bool, err error) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false, nil
	}
	if _, err := gitRun(dir, "add", "-A"); err != nil {
		return false, err
	}
	if out, err := gitRun(dir, "status", "--porcelain"); err != nil || out == "" {
		return false, err
	}
	args := []string{"commit", "-q", "-m", message}
	if email, _ := gitRun(dir, "config", "user.email"); email == "" {
		args = append([]string{"-c", "user.name=agctl", "-c", "user.email=agctl@localhost"}, args...)
	}
	if _, err := gitRun(dir, args...); err != nil {
		return false, err
	}
	return true, nil
}
