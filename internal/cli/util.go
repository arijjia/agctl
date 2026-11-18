package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// PrescanFlag достаёт значение флага из argv до штатного разбора flag:
// дефолты остальных флагов зависят от профиля, который задаётся этими же
// флагами. Понимает формы --name=v, --name v и -name v.
func PrescanFlag(args []string, name, def string) string {
	prefix := "--" + name + "="
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, prefix):
			return strings.TrimSpace(strings.TrimPrefix(a, prefix))
		case a == "--"+name && i+1 < len(args):
			return strings.TrimSpace(args[i+1])
		case a == "-"+name && i+1 < len(args):
			return strings.TrimSpace(args[i+1])
		}
	}
	return def
}

// DefaultHome возвращает дом данных agctl: $AGCTL_HOME, если задан,
// иначе ~/.agctl (или .agctl в текущем каталоге, если домашний каталог
// определить не удалось).
func DefaultHome() string {
	if h := os.Getenv("AGCTL_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agctl"
	}
	return filepath.Join(home, ".agctl")
}

// RequireHome создаёт каталог дома при необходимости и возвращает его
// абсолютный путь; ошибка создания оборачивается контекстом имени дома.
func RequireHome(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("home %s: %w", dir, err)
	}
	return filepath.Abs(dir)
}

// EnvOr возвращает значение переменной среды key или def, если она пуста
// либо не задана.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// FirstNonEmpty возвращает первую непустую строку из аргументов или пустую
// строку, если все пусты.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// NonZeroOr — численный аналог FirstNonEmpty для дефолтов из config.yaml:
// нулевое значение конфига означает «не задано» (0 в yaml — очевидный маркер
// отсутствия настройки, отдельный «нулевой» лимит смысла не имеет).
func NonZeroOr[T int | float64](v, def T) T {
	if v != 0 {
		return v
	}
	return def
}

// SplitCSV разбивает строку по запятым, обрезая пробелы и пропуская пустые
// элементы.
func SplitCSV(s string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// KeyState — состояние секрета для логов: только факт наличия, не значение.
func KeyState(secret string) string {
	if secret == "" {
		return "не задан"
	}
	return "задан"
}

// IsLoopbackAddr — true для адресов вида 127.0.0.1:p, [::1]:p, localhost:p.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DockerStatus возвращает человекочитаемую строку о доступности Docker:
// найден ли ожидаемый путь сокета/пайпа демона на текущей ОС.
func DockerStatus() string {
	if _, err := os.Stat(dockerHintPath()); err == nil {
		return "сокет найден"
	}
	return "если демон не запущен — стартуйте Docker Desktop / dockerd"
}

// dockerHintPath возвращает ожидаемый путь к сокету Docker для текущей
// платформы (macOS: ~/.docker/run/docker.sock, Windows: named pipe,
// прочие: /var/run/docker.sock) — только для подсказки в логе.
func dockerHintPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".docker", "run", "docker.sock")
	case "windows":
		return `\\.\pipe\docker_engine`
	default:
		return "/var/run/docker.sock"
	}
}

// EnvInt — целое из env или дефолт.
func EnvInt(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return def
}
