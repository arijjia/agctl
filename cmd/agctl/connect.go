package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func cmdConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	host := fs.String("host", "127.0.0.1", "адрес хоста agctl (публикуемый sshd привязан к loopback)")
	port := fs.Int("port", 0, "SSH-порт воркспейса (из GET /api/workspaces)")
	user := fs.String("user", "root", "SSH-пользователь внутри контейнера")
	identity := fs.String("identity", "", "путь к приватному ключу (опционально; иначе ssh-agent/default keys)")
	printOnly := fs.Bool("print", false, "только напечатать команду ssh, не запускать её")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *port < 1 || *port > 65535 {
		fmt.Fprintln(os.Stderr, "connect: укажи валидный --port из кабинета (1..65535)")
		os.Exit(2)
	}
	if strings.TrimSpace(*host) == "" || strings.HasPrefix(*host, "-") || strings.ContainsAny(*host, " \t\r\n") {
		fmt.Fprintln(os.Stderr, "connect: некорректный --host")
		os.Exit(2)
	}
	if strings.TrimSpace(*user) == "" || strings.ContainsAny(*user, " \t\r\n@") {
		fmt.Fprintln(os.Stderr, "connect: некорректный --user")
		os.Exit(2)
	}
	argv := connectCommand(*host, *port, *user, *identity)
	if *printOnly {
		fmt.Printf("ssh")
		for _, a := range argv {
			fmt.Printf(" %s", shellQuote(a))
		}
		fmt.Println()
		return
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect: ssh-клиент не найден в PATH")
		os.Exit(127)
	}
	cmd := exec.Command(ssh, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:@+-", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// testable helper keeps command rendering independent from exec.
func connectCommand(host string, port int, user, identity string) []string {
	argv := []string{"-p", strconv.Itoa(port)}
	if identity != "" {
		argv = append(argv, "-i", identity)
	}
	return append(argv, user+"@"+host)
}
