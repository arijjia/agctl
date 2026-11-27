package main

// dev_certs.go — `agctl dev certs`: корневые сертификаты организации для
// рабочих мест (localmode/certs.go). Набор собирается на каждом старте места
// и монтируется в контейнер; на хосте им пользуются llm-check и check.

import (
	"bufio"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
)

const devCertsUsage = `agctl dev certs — сертификаты организации для рабочих мест

  list                 🔍 что войдёт в набор: источники и число сертификатов (по умолчанию)
  host on|off|profile  ✏️ доверенные сертификаты этой машины в рабочем месте (config.yaml ca_host)
  fetch АДРЕС [--yes]  ✏️ цепочка сертификатов сервера (до корня по AIA): показать, сохранить в
                       <дом>/ca и добавить в набор — например шлюз моделей организации
  add ФАЙЛ.pem         ✏️ свой сертификат PEM в набор
  rm ИМЯ               ✏️ убрать файл из набора
  check АДРЕС          ⚠️ TLS до адреса: с доверием этой машины и с набором agctl

Набор применяется при перезапуске места. АДРЕС — https://хост[:порт]/… или хост[:порт].
`

// CABundle — набор сертификатов для рабочих мест (хук лаунчера).
func (h *devHooks) CABundle() (string, bool, error) {
	return localmode.WriteCABundle(h.home, h.cfg(), h.prof)
}

// devHostPort — host:port из адреса (https://…, хост, хост:порт).
func devHostPort(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, "://") {
		u, err := url.Parse(addr)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("адрес %q", addr)
		}
		addr = u.Host
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	return addr, nil
}

func devCerts(home string, args []string) error {
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	prof, _ := devResolveProfile(cfg)
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list":
		return devCertsList(home, cfg, prof)
	case "host":
		if len(args) != 1 {
			return errors.New("agctl dev certs host on|off|profile")
		}
		var v *bool
		switch args[0] {
		case "on", "off":
			on := args[0] == "on"
			v = &on
		case "profile":
		default:
			return errors.New("agctl dev certs host on|off|profile")
		}
		if err := localmode.SetCAHost(home, v); err != nil {
			return err
		}
		fmt.Println("✓ сохранено; применится при перезапуске места")
		cfg, _ = localmode.Load(home)
		return devCertsList(home, cfg, prof)
	case "fetch":
		fs := flag.NewFlagSet("dev certs fetch", flag.ContinueOnError)
		yes := fs.Bool("yes", false, "не спрашивать подтверждение")
		if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
			return errors.New("agctl dev certs fetch АДРЕС [--yes]")
		}
		return devCertsFetch(home, fs.Arg(0), *yes)
	case "add":
		if len(args) != 1 {
			return errors.New("agctl dev certs add ФАЙЛ.pem")
		}
		data, err := os.ReadFile(args[0]) // #nosec G304 -- файл, названный владельцем
		if err != nil {
			return err
		}
		path, err := localmode.SaveCAFile(home, args[0], data)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s (%d серт.) — в наборе; применится при перезапуске места\n", path, localmode.CountCerts(data))
		return nil
	case "rm":
		if len(args) != 1 {
			return errors.New("agctl dev certs rm ИМЯ")
		}
		ok, err := localmode.RemoveCAFile(home, args[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("в наборе нет %s (agctl dev certs list)", args[0])
		}
		fmt.Println("✓ убран из набора (файл в ca/ остался); применится при перезапуске места")
		return nil
	case "check":
		if len(args) != 1 {
			return errors.New("agctl dev certs check АДРЕС")
		}
		return devCertsCheck(home, cfg, prof, args[0])
	case "help", "-h", "--help":
		fmt.Print(devCertsUsage)
		return nil
	}
	return fmt.Errorf("неизвестная команда %q\n\n%s", sub, devCertsUsage)
}

func devCertsList(home string, cfg *localmode.Config, prof *profile.Data) error {
	p := localmode.PlanCABundle(home, cfg, prof)
	host := "выкл"
	if localmode.CAHostOn(cfg, prof) {
		host = "вкл"
	}
	src := "профиль"
	if cfg.CAHost != nil {
		src = "config.yaml"
	}
	fmt.Printf("доверенные сертификаты этой машины: %s (%s)\n", host, src)
	if !p.Enabled {
		fmt.Println("набор пуст — рабочее место доверяет только корням образа (Debian, Node)")
		return nil
	}
	total := 0
	for _, s := range p.Sources {
		if s.Err != "" {
			fmt.Printf("  ✗ %s: %s\n", s.Name, s.Err)
			continue
		}
		total += s.Certs
		fmt.Printf("  ✓ %s — %d\n", s.Name, s.Certs)
	}
	kind := "только Node (dsh, npx): NODE_EXTRA_CA_CERTS"
	if p.Full {
		kind = "полный: Node, Python, uv, pip, curl, git, Go (SSL_CERT_FILE и соседние)"
	}
	fmt.Printf("набор: %d серт. → %s в контейнере; %s\n", total, "/etc/agctl/ca-bundle.pem", kind)
	return nil
}

func devCertsFetch(home, addr string, yes bool) error {
	hp, err := devHostPort(addr)
	if err != nil {
		return err
	}
	chain, err := localmode.FetchChain(hp, 15*time.Second)
	if err != nil {
		return fmt.Errorf("%s: %w", hp, err)
	}
	fmt.Printf("цепочка %s (%d):\n", hp, len(chain))
	for i, c := range chain {
		d := localmode.DescribeCert(c)
		role := "сервер"
		switch {
		case d.SelfSigned:
			role = "корень"
		case i > 0:
			role = "промежуточный"
		}
		fmt.Printf("  %d. %s: %s\n     издатель: %s\n     до %s · SHA-256 %s\n", i+1, role, d.Subject, d.Issuer, d.NotAfter.Format("2006-01-02"), d.SHA256)
	}
	anchors := localmode.TrustAnchors(chain)
	if top := chain[len(chain)-1]; !localmode.DescribeCert(top).SelfSigned {
		fmt.Println("  ⚠ корень не найден (сервер его не отдал, адреса издателя в сертификате нет или он недоступен) — сохраню верхний сертификат цепочки; проверьте результат через check")
	}
	if !yes {
		fmt.Print("Доверять этим сертификатам в рабочих местах этого дома? Сверьте отпечаток с выданным администраторами. [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" && a != "д" && a != "да" {
			return errors.New("отменено")
		}
	}
	host, _, _ := net.SplitHostPort(hp)
	data := localmode.EncodeCerts(anchors)
	path, err := localmode.SaveCAFile(home, host, data)
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s (%d серт.) — в наборе; применится при перезапуске места\n", path, len(anchors))
	if _, withBundle := localmode.CheckTLS(hp, data, 15*time.Second); withBundle != nil {
		fmt.Printf("  ⚠ проверка TLS с ним не прошла: %v\n", withBundle)
	} else {
		fmt.Println("  ✓ проверка TLS с ним проходит")
	}
	return nil
}

func devCertsCheck(home string, cfg *localmode.Config, prof *profile.Data, addr string) error {
	hp, err := devHostPort(addr)
	if err != nil {
		return err
	}
	p := localmode.PlanCABundle(home, cfg, prof)
	withHost, withBundle := localmode.CheckTLS(hp, p.PEM(), 15*time.Second)
	show := func(label string, err error) {
		if err == nil {
			fmt.Printf("  ✓ %s\n", label)
			return
		}
		var unknown x509.UnknownAuthorityError
		hint := ""
		if errors.As(err, &unknown) {
			hint = " — сертификат организации не доверен: agctl dev certs fetch " + addr
		}
		fmt.Printf("  ✗ %s: %v%s\n", label, err, hint)
	}
	fmt.Printf("TLS до %s:\n", hp)
	show("доверие этой машины", withHost)
	show(fmt.Sprintf("набор agctl (%d серт. поверх доверия машины)", localmode.CountCerts(p.PEM())), withBundle)
	if withBundle != nil {
		return errors.New("рабочее место до этого адреса по TLS не дойдёт")
	}
	return nil
}
