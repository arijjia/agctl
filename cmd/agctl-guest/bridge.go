package main

// bridge.go — клиент моста к хосту изнутри контейнера рабочего места
// (локальный режим): agctl bridge list | call ИНСТРУМЕНТ k=v … | mcp.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arijjia/agctl/internal/localbridge"
)

func cmdBridge(args []string) {
	c := &localbridge.Client{URL: os.Getenv(localbridge.EnvURL), Token: os.Getenv(localbridge.EnvToken)}
	if c.URL == "" || c.Token == "" {
		// Команды агента идут с вычищенным env (dsh убирает *TOKEN*):
		// адрес и токен моста — в файле доступов dsh-home.
		c.URL, c.Token = bridgeFromFile()
	}
	if c.URL == "" || c.Token == "" {
		fmt.Fprintln(os.Stderr, "agctl bridge: мост не настроен (нет AGCTL_BRIDGE_URL/AGCTL_BRIDGE_TOKEN) — он есть только в локальном режиме agctl dev")
		os.Exit(1)
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	ctx := context.Background()
	var err error
	switch args[0] {
	case "list":
		var tools []localbridge.Tool
		if tools, err = c.List(ctx); err == nil {
			out, _ := json.MarshalIndent(map[string]any{"tools": tools}, "", "  ")
			fmt.Println(string(out))
		}
	case "call":
		if len(args) < 2 {
			err = errors.New("использование: agctl bridge call ИНСТРУМЕНТ имя=значение …")
			break
		}
		kv := map[string]string{}
		for _, a := range args[2:] {
			k, v, ok := strings.Cut(a, "=")
			if !ok {
				err = fmt.Errorf("аргумент %q: нужен вид имя=значение", a)
				break
			}
			kv[k] = v
		}
		if err != nil {
			break
		}
		var res localbridge.Result
		if res, err = c.Call(ctx, args[1], kv); err == nil {
			out, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(out))
			if res.ExitCode != 0 {
				os.Exit(3)
			}
		}
	case "mcp":
		err = localbridge.ServeMCP(ctx, os.Stdin, os.Stdout, c)
	default:
		err = fmt.Errorf("неизвестная команда %q (list | call | mcp)", args[0])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "agctl bridge: %v\n", err)
		os.Exit(1)
	}
}

func bridgeFromFile() (string, string) {
	home := os.Getenv("DSH_HOME")
	if home == "" {
		home = "/home/dsh"
	}
	raw, err := os.ReadFile(filepath.Join(home, ".agctl", "bridge.json"))
	if err != nil {
		return "", ""
	}
	var b struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return "", ""
	}
	return b.URL, b.Token
}
