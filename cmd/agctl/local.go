package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/arijjia/agctl/internal/bridge"
)

// cmdLocalConnect — experimental outbound connector for the user's machine:
// agctl local connect --server https://agctl.example --grant TOKEN [--launch-dsh]
// Локальный dsh остаётся на loopback; наружу — только исходящий WSS к agctl.
func cmdLocalConnect(args []string) {
	fs := flag.NewFlagSet("local connect", flag.ExitOnError)
	server := fs.String("server", envOr("AGCTL_LOCAL_SERVER", ""), "адрес платформы agctl (например, https://agctl.example)")
	grant := fs.String("grant", envOr("AGCTL_LOCAL_GRANT", ""), "одноразовый pairing grant из POST /api/local-backend/grant (лучше через env AGCTL_LOCAL_GRANT)")
	local := fs.String("local", "", "loopback-адрес локального dsh web (по умолчанию 127.0.0.1:<dsh-port>)")
	launchDSH := fs.Bool("launch-dsh", false, "запустить локальный dsh web и вычитывать boot-токен из его логов")
	dshBin := fs.String("dsh-bin", "dsh", "бинарь локального dsh для --launch-dsh")
	dshPort := fs.Int("dsh-port", 3080, "порт локального dsh web для --launch-dsh")
	dshToken := fs.String("dsh-token", envOr("AGCTL_LOCAL_DSH_TOKEN", ""), "boot-токен уже запущенного dsh, если известен (лучше env)")
	_ = fs.Parse(args)

	if *server == "" || *grant == "" {
		fmt.Fprintln(os.Stderr, "нужны --server (или AGCTL_LOCAL_SERVER) и --grant (или AGCTL_LOCAL_GRANT)")
		fs.Usage()
		os.Exit(2)
	}
	if strings.TrimSpace(*local) == "" {
		*local = "127.0.0.1:" + strconv.Itoa(*dshPort)
	}
	wsEndpoint, err := bridge.ConnectorWebSocketURL(*server)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tokens := make(chan string, 8)
	if *dshToken != "" {
		tokens <- *dshToken
	}
	if *launchDSH {
		dshCmd, pumpDone := launchLocalDSH(ctx, *dshBin, *dshPort, tokens)
		defer func() {
			cancel := dshCmd.Cancel // CommandContext: cancel останавливает процесс
			if cancel != nil {
				cancel()
			}
			<-pumpDone
		}()
	} else if *dshToken == "" {
		log.Printf("local: предупреждение — без --dsh-token/--launch-dsh локальный dsh ответит 401, пока его boot-токен не передан")
	}

	log.Printf("local: подключаюсь к %s (локальный dsh %s); Ctrl-C — отключить", *server, *local)
	if err := bridge.Connect(ctx, wsEndpoint, *grant, bridge.ConnectorOptions{
		LocalAddr:     *local,
		DSHBootTokens: tokens,
	}); err != nil {
		log.Fatalf("local: %v", err)
	}
	log.Printf("local: соединение закрыто")
}

// launchLocalDSH starts the local dsh web process and pumps its boot tokens.
func launchLocalDSH(ctx context.Context, bin string, port int, tokens chan<- string) (*exec.Cmd, <-chan struct{}) {
	cmd := exec.CommandContext(ctx, bin, "web", "--port", strconv.Itoa(port), "--no-open")
	logReader, logWriter, err := os.Pipe()
	if err != nil {
		log.Fatalf("local: log pipe: %v", err)
	}
	cmd.Stdout = io.MultiWriter(os.Stderr, logWriter)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatalf("local: запустить %q: %v", bin, err)
	}
	log.Printf("local: запущен %s web --port %d --no-open (pid %d)", bin, port, cmd.Process.Pid)
	pumpDone := make(chan struct{})
	go func() {
		bridge.PumpDSHTokens(ctx, logReader, tokens)
		_ = logWriter.Close()
		close(pumpDone)
	}()
	go func() {
		_ = cmd.Wait()
		_ = logReader.Close()
	}()
	return cmd, pumpDone
}
