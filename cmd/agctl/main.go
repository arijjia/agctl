// agctl — один бинарь: многопользовательская платформа агентских рабочих
// мест (dsh web в контейнере + персональная git-репка + бэклоги).
//
// Команды:
//
//	agctl serve     — поднять UI+API+provision (демо-режим «одной кнопки»)
//	agctl migrate   — накатить миграции SQLite вручную
//	agctl backlog … — CLI ведения бэклогов (формат v1, gitctl-совместимый)
//	agctl version   — версия и сборка
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/backlog"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
	"github.com/arijjia/agctl/web"
)

// version подставляется ldflags при сборке (make/goreleaser).
var version = "dev"

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "backlog":
		os.Exit(backlog.Main(os.Args[2:], os.Stdout, os.Stderr))
	case "workspace":
		cmdWorkspace(os.Args[2:])
	case "version":
		fmt.Printf("agctl %s (%s/%s %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `agctl — платформа агентских рабочих мест (один бинарь)

Команды:
  serve [--home DIR] [--addr H:P] [--image T] [--bin-dir D] [--max-per-user N]
  migrate [--home DIR]
  backlog <new|list|top|check|start|done|cancel|note|migrate> [флаги]
  workspace create --user ЛОГИН --name ИМЯ --template 1c-vibe --base-url URL --model M --api-key KEY
  version

Переменные окружения: AGCTL_HOME (по умолчанию ~/.agctl), AGCTL_ADDR.
`)
}

func defaultHome() string {
	if h := os.Getenv("AGCTL_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agctl"
	}
	return filepath.Join(home, ".agctl")
}

func requireHome(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("home %s: %w", dir, err)
	}
	return filepath.Abs(dir)
}

func cmdServe(args []string) {
	fsSet := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	addr := fsSet.String("addr", envOr("AGCTL_ADDR", "127.0.0.1:8929"), "адрес UI/API")
	image := fsSet.String("image", "agctl-dsh:0.1.0", "тег образа agctl-dsh")
	binDir := fsSet.String("bin-dir", "", "каталог linux-сборок agctl для образа (по умолчанию ./dist)")
	secureCookies := fsSet.Bool("secure-cookies", false, "выставлять Secure на cookie (если TLS завершается на доверенном ingress/reverse proxy)")
	maxPerUser := fsSet.Int("max-per-user", 3, "лимит воркспейсов на пользователя")
	memGB := fsSet.Float64("mem-gb", 2, "лимит памяти контейнера, ГБ")
	cpus := fsSet.Float64("cpus", 2, "лимит CPU контейнера")
	_ = fsSet.Parse(args)

	absHome, err := requireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	keyPath, err := secrets.EnsureMasterKey(absHome)
	if err != nil {
		log.Fatal(err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	srv, err := server.New(db, sec, server.Config{
		Home:          absHome,
		Addr:          *addr,
		MaxPerUser:    *maxPerUser,
		MemoryBytes:   int64(*memGB * 1024 * 1024 * 1024),
		NanoCPUs:      int64(*cpus * 1e9),
		Image:         *image,
		BinDir:        firstNonEmpty(*binDir, "dist"),
		SecureCookies: *secureCookies,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()

	webFS, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	srv.StartBackground(ctx)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(webFS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("agctl serve: home=%s addr=http://%s (UI и API)", absHome, *addr)
	log.Printf("docker: %s", dockerStatus())
	if err := httpSrv.ListenAndServe(); err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Fatal(err)
	}
}

func cmdMigrate(args []string) {
	fsSet := flag.NewFlagSet("migrate", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	_ = fsSet.Parse(args)
	absHome, err := requireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("migrate: схема актуальна:", filepath.Join(absHome, "agctl.db"))
}

func dockerStatus() string {
	if _, err := os.Stat(dockerHintPath()); err == nil {
		return "сокет найден"
	}
	return "если демон не запущен — стартуйте Docker Desktop / dockerd"
}

func dockerHintPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".docker", "run", "docker.sock")
	case "windows":
		return `\\.\pipe\docker_engine`
	default:
		return "/var/run/docker.sock"
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
