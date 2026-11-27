package localmode

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/profile"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCert(t *testing.T, tmpl *x509.Certificate, parent *testCA) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, parentCert := key, tmpl
	if parent != nil {
		signer, parentCert = parent.key, parent.cert
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testCA{cert: c, key: key}
}

func caTemplate(cn string, serial int64, aia string) *x509.Certificate {
	t := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	if aia != "" {
		t.IssuingCertificateURL = []string{aia}
	}
	return t
}

func pemOf(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

// Набор сертификатов: корни машины (ca_host из профиля или config.yaml),
// файлы профиля и дома; пустой план — файла нет; полный — только с корнями
// машины.
func TestCABundle(t *testing.T) {
	root := newCert(t, caTemplate("Org Root", 1, ""), nil)
	hostPEM := pemOf(newCert(t, caTemplate("Public Root", 2, ""), nil).cert)
	old := hostCAPEM
	hostCAPEM = func() ([]byte, string, error) { return hostPEM, "тестовая машина", nil }
	defer func() { hostCAPEM = old }()

	home := t.TempDir()
	if err := (&Config{}).Save(home); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load(home)
	if path, _, err := WriteCABundle(home, cfg, nil); err != nil || path != "" {
		t.Fatalf("пустой план: %q %v", path, err)
	}
	// профиль: ca_host и свой корень
	pdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pdir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(pdir, "assets", "root.pem"), pemOf(root.cert), 0o644)
	_ = os.WriteFile(filepath.Join(pdir, "profile.yaml"), []byte("schema: 1\nid: org\nbrand: {name: Org}\ntls:\n  ca_host: true\n  ca_files: [assets/root.pem]\n"), 0o644)
	prof, err := profile.LoadDir(pdir)
	if err != nil {
		t.Fatal(err)
	}
	path, full, err := WriteCABundle(home, cfg, prof)
	if err != nil || path == "" || !full {
		t.Fatalf("профиль: %q full=%v %v", path, full, err)
	}
	raw, _ := os.ReadFile(path)
	if CountCerts(raw) != 2 {
		t.Fatalf("в наборе %d серт.", CountCerts(raw))
	}
	// config.yaml сильнее профиля: без корней машины набор не полный
	off := false
	if err := SetCAHost(home, &off); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(home)
	if _, full, _ := WriteCABundle(home, cfg, prof); full {
		t.Fatal("ca_host: false — набор не полный")
	}
	// свой файл дома
	if _, err := SaveCAFile(home, "gw.example.org", pemOf(root.cert)); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCAFile(home, "junk", []byte("не сертификат")); err == nil {
		t.Fatal("мусор принят как сертификат")
	}
	cfg, _ = Load(home)
	plan := PlanCABundle(home, cfg, prof)
	if len(plan.Sources) != 2 || CountCerts(plan.PEM()) != 2 || cfg.CAFiles[0] != "gw.example.org.pem" {
		t.Fatalf("план: %+v files=%v", plan.Sources, cfg.CAFiles)
	}
	if ok, err := RemoveCAFile(home, "gw.example.org"); !ok || err != nil {
		t.Fatalf("rm: %v %v", ok, err)
	}
}

// Цепочка сервера достраивается до корня по AIA; с набором из неё TLS
// проверяется, с доверием машины — нет.
func TestFetchChain(t *testing.T) {
	root := newCert(t, caTemplate("Test Root", 10, ""), nil)
	aia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(root.cert.Raw) // DER, как отдают CA Issuers
	}))
	defer aia.Close()
	inter := newCert(t, caTemplate("Test Intermediate", 11, aia.URL+"/root.cer"), root)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(12), Subject: pkix.Name{CommonName: "gateway"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature, IssuingCertificateURL: []string{aia.URL + "/inter.cer"},
	}
	leaf := newCert(t, leafTmpl, inter)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leaf.cert.Raw, inter.cert.Raw}, PrivateKey: leaf.key}}}
	srv.StartTLS()
	defer srv.Close()
	hp := strings.TrimPrefix(srv.URL, "https://")

	chain, err := FetchChain(hp, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 || !DescribeCert(chain[2]).SelfSigned || chain[2].Subject.CommonName != "Test Root" {
		t.Fatalf("цепочка: %d, последний %v", len(chain), chain[len(chain)-1].Subject)
	}
	anchors := TrustAnchors(chain)
	if len(anchors) != 2 || anchors[0].Subject.CommonName != "Test Intermediate" {
		t.Fatalf("якоря: %v", anchors)
	}
	withHost, withBundle := CheckTLS(hp, EncodeCerts(anchors), 5*time.Second)
	if withHost == nil || withBundle != nil {
		t.Fatalf("проверка: машина=%v набор=%v", withHost, withBundle)
	}
	if d := DescribeCert(chain[0]); !strings.Contains(d.Subject, "gateway") || strings.Count(d.SHA256, ":") != 31 {
		t.Fatalf("описание: %+v", d)
	}
}

// Linux: корень организации, положенный в /etc/ssl/certs мимо общего файла
// (без update-ca-certificates), тоже попадает в набор; повторы — один раз,
// DER читается.
func TestLinuxHostCAPEM(t *testing.T) {
	a := newCert(t, caTemplate("Public Root", 1, ""), nil)
	b := newCert(t, caTemplate("Org Root CA", 2, ""), nil)
	c := newCert(t, caTemplate("Anchor DER", 3, ""), nil)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "ca-certificates.crt")
	certs := filepath.Join(dir, "certs")
	anchors := filepath.Join(dir, "anchors")
	for _, d := range []string{certs, anchors} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p string, data []byte) {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(bundle, pemOf(a.cert))
	write(filepath.Join(certs, "public.pem"), pemOf(a.cert)) // повтор из общего файла
	write(filepath.Join(certs, "org-root.pem"), pemOf(b.cert))
	write(filepath.Join(certs, "README"), []byte("не сертификат"))
	write(filepath.Join(anchors, "anchor.cer"), c.cert.Raw)
	data, from, err := linuxHostCAPEM([]string{filepath.Join(dir, "нет"), bundle}, []string{certs, anchors, filepath.Join(dir, "нет-каталога")})
	if err != nil {
		t.Fatal(err)
	}
	if n := CountCerts(data); n != 3 {
		t.Fatalf("сертификатов %d, ждали 3 (%s)", n, from)
	}
	for _, want := range []string{bundle, certs + " (+1)", anchors + " (+1)"} {
		if !strings.Contains(from, want) {
			t.Errorf("источники %q без %q", from, want)
		}
	}
	if _, _, err := linuxHostCAPEM([]string{filepath.Join(dir, "нет")}, nil); err == nil {
		t.Fatal("пустое доверие принято")
	}
}

// «Как в рабочем месте»: доверие только набору — чужой корень не проходит,
// свой проходит.
func TestCheckTLSOnly(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	hp := strings.TrimPrefix(srv.URL, "https://")
	if err := CheckTLSOnly(hp, pemOf(srv.Certificate()), 5*time.Second); err != nil {
		t.Fatalf("свой сертификат: %v", err)
	}
	other := newCert(t, caTemplate("Other Root", 9, ""), nil)
	if err := CheckTLSOnly(hp, pemOf(other.cert), 5*time.Second); err == nil {
		t.Fatal("чужой корень принят")
	}
}
