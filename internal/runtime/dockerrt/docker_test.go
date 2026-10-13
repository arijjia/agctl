package dockerrt

import (
	"net"
	"testing"
)

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
