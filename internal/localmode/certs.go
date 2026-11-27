package localmode

// certs.go — корневые сертификаты для рабочего места. В корпоративной сети
// шлюз моделей, Jira, GitLab подписаны сертификатом организации, которому
// образ рабочего места (Debian + Node) не доверяет: dsh не дойдёт до шлюза,
// uvx, git и curl — до своих серверов. agctl собирает набор сертификатов и
// монтирует его в контейнер только для чтения (dockerrt: NODE_EXTRA_CA_CERTS
// для Node, SSL_CERT_FILE и соседние — для остальных, если набор полный).
//
// Источники: доверенные сертификаты этой машины (ca_host: там уже лежат
// сертификаты организации, установленные администраторами), файлы профиля
// (tls.ca_files) и дома (ca_files) — в том числе полученные командой
// agctl dev certs fetch прямо с сервера (цепочка достраивается по AIA).

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/profile"
)

// CADir — каталог сертификатов дома (agctl dev certs fetch|add).
func CADir(home string) string { return filepath.Join(home, "ca") }

// CABundlePath — собранный набор сертификатов, который монтируется в
// контейнер (пересобирается на каждом старте места).
func CABundlePath(home string) string { return filepath.Join(home, "run", "ca-bundle.pem") }

// hostCAPEM — доверенные сертификаты этой машины одним PEM: Linux — системный
// набор (Debian/Ubuntu, RHEL, Alpine), macOS — связки System и системных
// корней. Переменная — для тестов.
var hostCAPEM = func() ([]byte, string, error) {
	switch runtime.GOOS {
	case "linux":
		for _, p := range []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/cert.pem"} {
			if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte("BEGIN CERTIFICATE")) {
				return b, p, nil
			}
		}
		return nil, "", errors.New("системный набор сертификатов не найден (/etc/ssl/certs/ca-certificates.crt)")
	case "darwin":
		var out []byte
		for _, kc := range []string{"/System/Library/Keychains/SystemRootCertificates.keychain", "/Library/Keychains/System.keychain"} {
			if b, err := exec.Command("security", "find-certificate", "-a", "-p", kc).Output(); err == nil {
				out = append(out, b...)
			}
		}
		if !bytes.Contains(out, []byte("BEGIN CERTIFICATE")) {
			return nil, "", errors.New("security find-certificate ничего не выгрузил")
		}
		return out, "связки ключей macOS (System, системные корни)", nil
	}
	return nil, "", fmt.Errorf("доверенные сертификаты этой машины на %s не выгружаются — добавьте нужные файлом (agctl dev certs add)", runtime.GOOS)
}

// CASource — источник сертификатов набора (для agctl dev certs list).
type CASource struct {
	Name  string // откуда
	Certs int    // сколько сертификатов
	Err   string // почему не взят
}

// CAPlan — что войдёт в набор: включён ли он, полный ли (есть корни хоста),
// источники.
type CAPlan struct {
	Enabled bool
	Full    bool
	Sources []CASource
	pem     []byte
}

// CAHostOn — брать ли доверенные сертификаты хоста: config.yaml ca_host, иначе
// tls.ca_host профиля.
func CAHostOn(cfg *Config, prof *profile.Data) bool {
	if cfg.CAHost != nil {
		return *cfg.CAHost
	}
	return prof != nil && prof.TLS.CAHost
}

// PlanCABundle — что войдёт в набор сертификатов дома. Пустой план
// (Enabled=false) — ничего не настроено: контейнер без изменений.
func PlanCABundle(home string, cfg *Config, prof *profile.Data) CAPlan {
	var p CAPlan
	add := func(name string, data []byte, err error) {
		if err != nil {
			p.Sources = append(p.Sources, CASource{Name: name, Err: err.Error()})
			return
		}
		n := CountCerts(data)
		if n == 0 {
			p.Sources = append(p.Sources, CASource{Name: name, Err: "в файле нет сертификатов PEM"})
			return
		}
		p.Sources = append(p.Sources, CASource{Name: name, Certs: n})
		p.pem = append(p.pem, bytes.TrimSpace(data)...)
		p.pem = append(p.pem, '\n')
	}
	if CAHostOn(cfg, prof) {
		data, from, err := hostCAPEM()
		add("эта машина: "+from, data, err)
		p.Full = err == nil
	}
	if prof != nil && prof.View().FS != nil {
		for _, f := range prof.TLS.CAFiles {
			data, err := fs.ReadFile(prof.View().FS, f)
			add("профиль "+prof.ID+": "+f, data, err)
		}
	}
	for _, f := range cfg.CAFiles {
		path := f
		if !filepath.IsAbs(path) {
			path = filepath.Join(CADir(home), f)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- файл, названный владельцем дома
		add("дом: "+path, data, err)
	}
	p.Enabled = len(p.pem) > 0
	return p
}

// WriteCABundle собирает набор сертификатов дома в CABundlePath. Пустой план
// — файл убирается, path = "".
func WriteCABundle(home string, cfg *Config, prof *profile.Data) (path string, full bool, err error) {
	p := PlanCABundle(home, cfg, prof)
	target := CABundlePath(home)
	if !p.Enabled {
		_ = os.Remove(target)
		return "", false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", false, err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, p.pem, 0o644); err != nil { // #nosec G306 -- публичные сертификаты, читает контейнер
		return "", false, err
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", false, err
	}
	return target, p.Full, nil
}

// CountCerts — сколько сертификатов в PEM.
func CountCerts(data []byte) int {
	n := 0
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			return n
		}
		if b.Type == "CERTIFICATE" {
			n++
		}
	}
}

// CertInfo — сертификат по-человечески (agctl dev certs fetch|check).
type CertInfo struct {
	Subject, Issuer string
	NotAfter        time.Time
	SHA256          string
	CA, SelfSigned  bool
}

// DescribeCert — субъект, издатель, срок, отпечаток SHA-256.
func DescribeCert(c *x509.Certificate) CertInfo {
	sum := sha256.Sum256(c.Raw)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	var parts []string
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return CertInfo{
		Subject: c.Subject.String(), Issuer: c.Issuer.String(), NotAfter: c.NotAfter,
		SHA256: strings.Join(parts, ":"), CA: c.IsCA, SelfSigned: selfSigned(c),
	}
}

func selfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(c) == nil
}

// FetchChain — цепочка сертификатов сервера host:port, как он её отдаёт
// (без проверки доверия), достроенная до корня по адресам издателей из самих
// сертификатов (AIA, CA Issuers) — серверы обычно не отдают корень. Первый
// элемент — сертификат сервера.
func FetchChain(hostport string, timeout time.Duration) ([]*x509.Certificate, error) {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: timeout}
	// #nosec G402 -- цель — получить цепочку, которой ещё не доверяем; доверие решает человек по отпечатку
	conn, err := tls.DialWithDialer(d, "tcp", hostport, &tls.Config{ServerName: host, InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	chain := conn.ConnectionState().PeerCertificates
	_ = conn.Close()
	if len(chain) == 0 {
		return nil, errors.New("сервер не отдал сертификаты")
	}
	client := &http.Client{Timeout: timeout}
	for i := 0; i < 5; i++ {
		last := chain[len(chain)-1]
		if selfSigned(last) || len(last.IssuingCertificateURL) == 0 {
			break
		}
		parent, err := fetchIssuer(client, last)
		if err != nil {
			break // достроить не вышло — сохраним то, что есть
		}
		chain = append(chain, parent)
	}
	return chain, nil
}

// fetchIssuer — сертификат издателя по AIA (DER или PEM).
func fetchIssuer(client *http.Client, c *x509.Certificate) (*x509.Certificate, error) {
	var last error
	for _, u := range c.IssuingCertificateURL {
		resp, err := client.Get(u) // #nosec G107 -- адрес из сертификата, только чтение публичного сертификата
		if err != nil {
			last = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if b, _ := pem.Decode(data); b != nil {
			data = b.Bytes
		}
		parent, err := x509.ParseCertificate(data)
		if err != nil {
			last = err
			continue
		}
		if c.CheckSignatureFrom(parent) != nil {
			last = fmt.Errorf("%s: не издатель %s", u, c.Subject)
			continue
		}
		return parent, nil
	}
	if last == nil {
		last = errors.New("нет адресов издателя")
	}
	return nil, last
}

// TrustAnchors — что сохранить из цепочки сервера: всё, кроме сертификата
// самого сервера (промежуточные и корень, если он достроен).
func TrustAnchors(chain []*x509.Certificate) []*x509.Certificate {
	if len(chain) <= 1 {
		return chain
	}
	return chain[1:]
}

// EncodeCerts — сертификаты в PEM.
func EncodeCerts(certs []*x509.Certificate) []byte {
	var b bytes.Buffer
	for _, c := range certs {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return b.Bytes()
}

// CheckTLS — проверить TLS до host:port: с доверием этой машины и с набором
// pemExtra поверх него. nil — проверка прошла.
func CheckTLS(hostport string, pemExtra []byte, timeout time.Duration) (withHost, withBundle error) {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return err, err
	}
	d := &net.Dialer{Timeout: timeout}
	dial := func(pool *x509.CertPool) error {
		conn, err := tls.DialWithDialer(d, "tcp", hostport, &tls.Config{ServerName: host, RootCAs: pool, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		return conn.Close()
	}
	withHost = dial(nil)
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(pemExtra)
	withBundle = dial(pool)
	return withHost, withBundle
}

// PEM — собранный набор (для проверок на хосте: agctl dev llm-check, check).
func (p CAPlan) PEM() []byte { return p.pem }

// SetCAHost — config.yaml ca_host (nil — как в профиле).
func SetCAHost(home string, on *bool) error {
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	cfg.CAHost = on
	return cfg.Save(home)
}

// SaveCAFile кладёт сертификаты PEM в <дом>/ca/<name>.pem и добавляет файл
// в config.yaml ca_files. Возвращает путь.
func SaveCAFile(home, name string, data []byte) (string, error) {
	if CountCerts(data) == 0 {
		return "", errors.New("в данных нет сертификатов PEM")
	}
	name = strings.TrimSuffix(filepath.Base(name), ".pem") + ".pem"
	if strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("имя файла %q", name)
	}
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(CADir(home), 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(CADir(home), name)
	if err := os.WriteFile(path, data, 0o644); err != nil { // #nosec G306 -- публичные сертификаты
		return "", err
	}
	for _, f := range cfg.CAFiles {
		if f == name {
			return path, nil
		}
	}
	cfg.CAFiles = append(cfg.CAFiles, name)
	return path, cfg.Save(home)
}

// RemoveCAFile убирает файл из config.yaml ca_files (сам файл в ca/ остаётся).
func RemoveCAFile(home, name string) (bool, error) {
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return false, err
	}
	out, found := cfg.CAFiles[:0], false
	for _, f := range cfg.CAFiles {
		if f == name || f == name+".pem" || filepath.Base(f) == name {
			found = true
			continue
		}
		out = append(out, f)
	}
	cfg.CAFiles = out
	if !found {
		return false, nil
	}
	return true, cfg.Save(home)
}
