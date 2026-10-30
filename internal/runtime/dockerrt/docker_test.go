package dockerrt

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// TestPinnedHost — пин ExtraHosts по baseURL провайдера: корпоративный хост
// пинится на IPv4 хост-резолва, localhost/IP/пустой URL — не пинятся,
// ошибка резолва возвращается как ошибка (решение о старте — за Start).
func TestPinnedHost(t *testing.T) {
	lookup := func(host string) ([]net.IP, error) {
		if host == "llm.corp.example" {
			return []net.IP{net.ParseIP("10.255.254.41"), net.ParseIP("fd00::41")}, nil
		}
		return nil, fmt.Errorf("no such host %s", host)
	}
	cases := []struct {
		baseURL string
		want    string
		wantErr bool
	}{
		{"https://llm.corp.example/v1", "llm.corp.example:10.255.254.41", false},
		{"http://127.0.0.1:4000/v1", "", false},
		{"http://localhost:4000/v1", "", false},
		{"https://10.255.254.41/v1", "", false},
		{"https://corp.example/v1", "", true},
		{"", "", false},
	}
	for _, c := range cases {
		got, err := pinnedHost(c.baseURL, lookup)
		if c.wantErr != (err != nil) {
			t.Errorf("pinnedHost(%q): err=%v, wantErr=%v", c.baseURL, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("pinnedHost(%q) = %q, ждали %q", c.baseURL, got, c.want)
		}
	}
}

func TestPinsForBaseURLs(t *testing.T) {
	calls := map[string]int{}
	lookup := func(host string) ([]net.IP, error) {
		calls[host]++
		switch host {
		case "corp-a.example":
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		case "corp-b.example":
			return []net.IP{net.ParseIP("10.0.0.2")}, nil
		default:
			return nil, fmt.Errorf("no such host %s", host)
		}
	}
	pins, errs := pinsForBaseURLs([]string{
		"https://corp-a.example/v1", "https://corp-b.example/v1",
		"https://corp-a.example/other", "http://127.0.0.1:4000/v1", "https://bad.example/v1",
	}, lookup)
	if strings.Join(pins, ",") != "corp-a.example:10.0.0.1,corp-b.example:10.0.0.2" {
		t.Fatalf("pins = %v", pins)
	}
	if len(errs) != 1 || calls["corp-a.example"] != 1 {
		t.Fatalf("lookup errors=%v calls=%v", errs, calls)
	}
}

// TestFreePort — порт из FreePort можно забиндить (он вернулся освобождённым)
// и он в валидном диапазоне.
func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if port <= 0 || port >= 65536 {
		t.Fatalf("некорректный порт %d", port)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoaTest(port))
	if err != nil {
		t.Fatalf("порт %d нельзя забиндить после FreePort: %v", port, err)
	}
	_ = ln.Close()
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestNewWithoutDockerFailsFast — без доступного docker-сокета New возвращает
// ошибку (а не панику); на машинах с docker проверяем ещё и успешный клиент.
func TestNewSmoke(t *testing.T) {
	rt, err := New(Config{Image: "agctl-dsh:test"}, nil)
	if err != nil {
		t.Skipf("docker недоступен на этой машине: %v", err)
	}
	if rt == nil {
		t.Fatal("New вернул nil без ошибки")
	}
	_ = rt.Close()
}
