package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/arijjia/agctl/internal/bridge"
)

// cmdLocalConnect — experimental outbound connector for the user's machine:
// agctl local connect --server https://agctl.example --grant TOKEN [--launch-dsh]
// Локальный dsh остаётся на loopback; наружу — только исходящий WSS к agctl.
// После первого pairing сервер выдаёт durable credential — она сохраняется в
// --credential-file (0600) и используется для автопереподключений до revoke.
func cmdLocalConnect(args []string) {
	fs := flag.NewFlagSet("local connect", flag.ExitOnError)
	server := fs.String("server", envOr("AGCTL_LOCAL_SERVER", ""), "адрес платформы agctl (например, https://agctl.example)")
	grant := fs.String("grant", envOr("AGCTL_LOCAL_GRANT", ""), "одноразовый pairing grant из POST /api/local-backend/grant (лучше через env AGCTL_LOCAL_GRANT)")
	credentialFile := fs.String("credential-file", "", "файл durable-credential (0600); по умолчанию ~/.agctl-local/connector-<host>-<port>.json")
	local := fs.String("local", "", "loopback-адрес локального dsh web (по умолчанию 127.0.0.1:<dsh-port>)")
	launchDSH := fs.Bool("launch-dsh", false, "запустить локальный dsh web и вычитывать boot-токен из его логов")
	dshBin := fs.String("dsh-bin", "dsh", "бинарь локального dsh для --launch-dsh")
	dshPort := fs.Int("dsh-port", 3080, "порт локального dsh web для --launch-dsh")
	dshToken := fs.String("dsh-token", envOr("AGCTL_LOCAL_DSH_TOKEN", ""), "boot-токен уже запущенного dsh, если известен (лучше env)")
	_ = fs.Parse(args)
	credPath := *credentialFile
	if credPath == "" {
		credPath = defaultCredentialFile(*server)
	}
	credential := readStoredCredential(credPath)
	if credential == "" && *server == "" {
		fmt.Fprintln(os.Stderr, "нужен --server (или AGCTL_LOCAL_SERVER)")
		fs.Usage()
		os.Exit(2)
	}
	if credential == "" && *grant == "" {
		fmt.Fprintln(os.Stderr, "нужен --grant (или AGCTL_LOCAL_GRANT) при первом подключении; при повторном — сохранённая credential в "+credPath)
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
	if credential != "" {
		log.Printf("local: использую сохранённую credential %s (новый grant не нужен)", credPath)
	} else {
		credential = *grant
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tokens := make(chan string, 8)
	if *dshToken != "" {
		tokens <- *dshToken
	}
	var dshCmd *exec.Cmd
	var pumpDone <-chan struct{}
	if *launchDSH {
		dshCmd, pumpDone = launchLocalDSH(ctx, *dshBin, *dshPort, tokens)
		defer func() {
			if dshCmd.Cancel != nil {
				dshCmd.Cancel()
			}
			<-pumpDone
		}()
	} else if *dshToken == "" {
		log.Printf("local: предупреждение — без --dsh-token/--launch-dsh локальный dsh ответит 401, пока его boot-токен не передан")
	}

	opts := bridge.ConnectorOptions{
		LocalAddr:     *local,
		DSHBootTokens: tokens,
		OnConnectorToken: func(token string) {
			if err := storeCredentialFile(credPath, *server, token); err != nil {
				log.Printf("local: НЕ удалось сохранить credential в %s: %v", credPath, err)
				return
			}
			log.Printf("local: durable credential сохранена в %s — reconnect больше не требует grant", credPath)
			credential = token
		},
	}

	log.Printf("local: подключаюсь к %s (локальный dsh %s); Ctrl-C — отключить", *server, *local)
	backoff := time.Second
	for ctx.Err() == nil {
		err := bridge.Connect(ctx, wsEndpoint, credential, opts)
		if err == nil || ctx.Err() != nil {
			break
		}
		// Auth rejection means the credential was revoked or the one-time
		// grant expired: reconnecting cannot help until the user re-pairs.
		if isAuthRejected(err) {
			log.Fatalf("local: сервер отклонил авторизацию (401/403): %v\nЕсли credential отозвана — удали %s и создай новый grant.", err, credPath)
		}
		log.Printf("local: соединение потеряно (%v); переподключение через %s", err, backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
	log.Printf("local: соединение закрыто")
	if dshCmd != nil && dshCmd.Process != nil {
		_ = dshCmd.Process.Signal(syscall.SIGTERM)
	}
}

// isAuthRejected отличает отказ авторизации от сетевого сбоя: при 401/403
// backoff-ретраи бессмысленны, пользователю нужен новый pairing.
func isAuthRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(msg, "403")
}

type storedCredential struct {
	Server         string `json:"server"`
	ConnectorToken string `json:"connector_token"`
}

func defaultCredentialFile(serverURL string) string {
	base, err := os.UserHomeDir()
	if err != nil || base == "" {
		base = "."
	}
	host, port := serverURL, ""
	if parsed, err := url.Parse(serverURL); err == nil && parsed.Host != "" {
		host, port = parsed.Hostname(), parsed.Port()
	}
	safe := strings.NewReplacer(":", "-", ".", "-", "/", "-").Replace(host + "-" + port)
	return filepath.Join(base, ".agctl-local", "connector-"+safe+".json")
}

func readStoredCredential(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var stored storedCredential
	if json.Unmarshal(data, &stored) != nil || stored.ConnectorToken == "" {
		return ""
	}
	return stored.ConnectorToken
}

func storeCredentialFile(path, serverURL, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(storedCredential{Server: serverURL, ConnectorToken: token}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
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
