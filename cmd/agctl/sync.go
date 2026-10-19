package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cmdSync(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: agctl sync <create|list|pause|resume|flush|terminate> ...")
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		cmdSyncCreate(args[1:])
	case "list", "pause", "resume", "flush", "terminate":
		runMutagen(append([]string{"sync", args[0]}, args[1:]...))
	default:
		fmt.Fprintf(os.Stderr, "sync: неизвестная команда %q\n", args[0])
		os.Exit(2)
	}
}

func cmdSyncCreate(args []string) {
	fs := flag.NewFlagSet("sync create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "", "уникальное имя Mutagen-сессии")
	local := fs.String("local", "", "локальная директория проекта (корень не должен быть symlink)")
	host := fs.String("host", "127.0.0.1", "SSH-хост, доступный с этой машины")
	port := fs.Int("port", 0, "sshPort из API/кабинета")
	user := fs.String("user", "root", "SSH-пользователь контейнера")
	printOnly := fs.Bool("print", false, "только вывести mutagen command")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *name == "" || strings.ContainsAny(*name, " \t\r\n") {
		fmt.Fprintln(os.Stderr, "sync create: укажи --name без пробелов")
		os.Exit(2)
	}
	if *port < 1 || *port > 65535 {
		fmt.Fprintln(os.Stderr, "sync create: укажи валидный --port из кабинета (1..65535)")
		os.Exit(2)
	}
	if strings.TrimSpace(*host) == "" || strings.HasPrefix(*host, "-") || strings.ContainsAny(*host, " \t\r\n") {
		fmt.Fprintln(os.Stderr, "sync create: некорректный --host")
		os.Exit(2)
	}
	if strings.TrimSpace(*user) == "" || strings.ContainsAny(*user, " \t\r\n@:") {
		fmt.Fprintln(os.Stderr, "sync create: некорректный --user")
		os.Exit(2)
	}
	if *local == "" {
		fmt.Fprintln(os.Stderr, "sync create: укажи --local <директория>")
		os.Exit(2)
	}
	localPath, err := filepath.Abs(*local)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sync create:", err)
		os.Exit(2)
	}
	info, err := os.Lstat(localPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintln(os.Stderr, "sync create: --local должен быть существующей директорией, не symlink")
		os.Exit(2)
	}
	argv := syncCreateArgs(*name, localPath, *user, *host, *port)
	if *printOnly {
		fmt.Printf("mutagen")
		for _, a := range argv {
			fmt.Printf(" %s", shellQuote(a))
		}
		fmt.Println()
		return
	}
	runMutagen(argv)
}

func syncCreateArgs(name, localPath, user, host string, port int) []string {
	remote := fmt.Sprintf("%s@%s:%d:/workspace", user, host, port)
	return []string{
		"sync", "create", "--name", name, "--sync-mode", "two-way-safe", "--ignore-vcs",
		"--ignore", "node_modules", "--ignore", ".env", "--ignore", ".env.*",
		"--ignore", ".credentials.yaml", "--ignore", ".credentials.yml", "--ignore", ".ssh",
		localPath, remote,
	}
}

func runMutagen(args []string) {
	binary, err := exec.LookPath("mutagen")
	if err != nil {
		fmt.Fprintln(os.Stderr, "agctl sync: Mutagen не найден; установи Mutagen CLI и повтори")
		os.Exit(127)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "agctl sync:", err)
		os.Exit(1)
	}
}
