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

// TestAgctlDrifted — дрейф по фактическому образу: make up пересобирает образ
// под тем же тегом, сравнивать нужно sha, нормализуя sha256:-префиксы.
func TestAgctlDrifted(t *testing.T) {
	const fresh = "sha256:aaaa1111"
	cases := []struct {
		name      string
		container string
		current   string
		wantDrift bool
	}{
		{"тот же образ", "sha256:aaaa1111", fresh, false},
		{"другая сборка того же тега", "sha256:bbbb2222", fresh, true},
		{"префиксы с одной стороны", "aaaa1111", fresh, false},
		{"пустой образ контейнера", "", fresh, true},
		{"текущий неизвестен — не считаем дрейфом", "sha256:bbbb2222", "", false},
	}
	for _, c := range cases {
		if got := agctlDrifted(c.container, c.current); got != c.wantDrift {
			t.Errorf("%s: agctlDrifted(%q, %q) = %v, ждали %v", c.name, c.container, c.current, got, c.wantDrift)
		}
	}
}

// TestSweepCandidate — свип трогает только agctl-контейнеры этого дома,
// работающие не на ЖЕЛАЕМОМ образе (метка agctl.image.desired перекрывает
// глобальный); чужие и свежие сборки проходят мимо.
func TestSweepCandidate(t *testing.T) {
	const prefix = "agctl-chat-ab12-"
	const fresh = "sha256:aaaa1111"
	ours := map[string]string{labelWorkspace: "chat-ab12-u1"}
	foreign := map[string]string{"app": "x"}
	// resolveID для желаемых образов теста: только этот дайджест известен.
	resolver := func(ref string) string {
		if ref == "sha256:cccc3333" {
			return ref
		}
		return ""
	}
	cases := []struct {
		name      string
		names     []string
		image     string
		labels    map[string]string
		wantSweep bool
	}{
		{"наш дрейф-хвост", []string{"/" + prefix + "u7"}, "sha256:bbbb2222", ours, true},
		{"наш контейнер без sha-префикса", []string{prefix + "u7"}, "bbbb2222", ours, true},
		{"наша свежая сборка", []string{"/" + prefix + "u7"}, fresh, ours, false},
		{"чужой контейнер с тем же именем", []string{"/" + prefix + "u7"}, "sha256:bbbb2222", foreign, false},
		{"наш дрейф, но другой дом", []string{"/agctl-chat-cd34-u1"}, "sha256:bbbb2222", ours, false},
		{"имя не совпало", []string{"/agctl-other"}, "sha256:bbbb2222", ours, false},
		// per-user образ: контейнер на желаемом образе (≠ глобальному) — не дрейф
		{"launched на желаемом образе", []string{"/" + prefix + "u7"}, "sha256:cccc3333",
			map[string]string{labelWorkspace: "chat-ab12-u1", labelImageDesired: "sha256:cccc3333"}, false},
		// per-user образ сменился: контейнер на старом желаемом — дрейф
		{"launched сменил образ", []string{"/" + prefix + "u7"}, "sha256:bbbb2222",
			map[string]string{labelWorkspace: "chat-ab12-u1", labelImageDesired: "sha256:cccc3333"}, true},
		// желаемый образ неизвестен резолверу — не трогаем
		{"желаемый образ не резолвится", []string{"/" + prefix + "u7"}, "sha256:bbbb2222",
			map[string]string{labelWorkspace: "chat-ab12-u1", labelImageDesired: "sha256:gone"}, false},
	}
	for _, c := range cases {
		if got := sweepCandidate(c.names, c.image, c.labels, prefix, fresh, resolver); got != c.wantSweep {
			t.Errorf("%s: sweepCandidate = %v, ждали %v", c.name, got, c.wantSweep)
		}
	}
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
