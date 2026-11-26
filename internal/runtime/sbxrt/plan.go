package sbxrt

// plan.go — чистые функции: имя песочницы, аргументы sbx create, сетевые
// правила, подмена ключей моделей заглушками, файл окружения. Каждый флаг
// CLI взят из справочника docs.docker.com/reference/cli/sbx (см. sbxrt.go).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

// SandboxName — имя песочницы по RuntimeID. Правила sbx create --name: 2–63
// символа ASCII (буквы, цифры, дефис, точка), начало и конец — буква или
// цифра, «default» зарезервировано.
func SandboxName(runtimeID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(runtimeID) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if len(name) < 2 || name == "default" {
		name = strings.Trim("agctl-"+name, "-")
	}
	return name
}

// createArgs — аргументы `sbx create` рабочего места: шаблон — образ
// рабочего места из хранилища sbx (--pull never: шаблон загружен нами, в
// реестр ходить не надо), агент shell (песочница без встроенного агента),
// первая папка — служебная основная (sbx монтирует её по пути хоста),
// остальные — PATH:TARGET (дом dsh, /workspace и папки рабочего места).
func createArgs(name, image, primary string, mounts []string, nanoCPUs, memoryBytes int64) []string {
	args := []string{"create", "--name", name, "--template", image, "--pull", "never"}
	if nanoCPUs > 0 {
		args = append(args, "--cpus", strconv.FormatInt((nanoCPUs+999_999_999)/1_000_000_000, 10))
	}
	if mib := memoryBytes >> 20; mib > 0 {
		args = append(args, "--memory", strconv.FormatInt(max(mib, 512), 10)+"m")
	}
	args = append(args, "shell", primary)
	return append(args, mounts...)
}

// networkResources — список разрешённых адресов песочницы (в sbx исходящая
// сеть запрещена по умолчанию): эндпоинты моделей, URL из окружения рабочего
// места (GitLab, Jira, мост к хосту и пр.) и белый список extra (egress.allow
// профиля и dev.yaml: хосты, IP, CIDR). host.docker.internal прокси sbx
// превращает в localhost — в правило идёт localhost:<порт>.
func networkResources(p provision.Provider, env, extra []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(res string) {
		if res != "" && !seen[res] {
			seen[res] = true
			out = append(out, res)
		}
	}
	for _, raw := range p.AllProviderURLs() {
		add(urlResource(raw))
	}
	for _, kv := range env {
		if _, v, ok := strings.Cut(kv, "="); ok {
			add(urlResource(v))
		}
	}
	for _, res := range extra {
		add(strings.TrimSpace(res))
	}
	sort.Strings(out)
	return out
}

// urlResource — host:port для правила сети по http(s)-URL; не URL — "".
func urlResource(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	if host == "host.docker.internal" || host == "127.0.0.1" || host == "::1" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// keySecret — ключ модели, который не должен попасть в VM: в песочнице
// вместо него заглушка, настоящий ключ прокси sbx подставляет в заголовки
// запросов к hosts (sbx secret set-custom).
type keySecret struct {
	name        string
	hosts       []string
	value       string
	placeholder string
}

// substituteKeys заменяет ключи провайдеров заглушками: возвращает
// провайдера для .credentials.yaml песочницы и список секретов для sbx.
// Ключ без известного хоста оставляется как есть (подставлять некуда).
func substituteKeys(sandbox string, p provision.Provider) (provision.Provider, []keySecret) {
	var secrets []keySecret
	sub := func(alias, key string, urls []string) string {
		var hosts []string
		for _, raw := range urls {
			if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Hostname() != "" {
				hosts = append(hosts, u.Hostname())
			}
		}
		if key == "" || len(hosts) == 0 {
			return key
		}
		sum := sha256.Sum256([]byte(sandbox + "\x00" + alias))
		s := keySecret{name: "agctl-llm-" + alias, hosts: hosts, value: key, placeholder: "agctl-sbx-" + hex.EncodeToString(sum[:12])}
		secrets = append(secrets, s)
		return s.placeholder
	}
	out := p
	out.APIKey = sub("main", p.APIKey, p.AllBaseURLs())
	out.Extras = make([]provision.ExtraProvider, len(p.Extras))
	for i, e := range p.Extras {
		e.APIKey = sub("extra"+strconv.Itoa(i), e.APIKey, []string{e.BaseURL})
		out.Extras[i] = e
	}
	return out, secrets
}

// envFileContent — файл для `sbx exec --env-file`: значения доступов не
// попадают в аргументы процесса на хосте. Значение, равное ключу модели,
// заменяется его заглушкой.
func envFileContent(env []string, secrets []keySecret) (string, error) {
	var b strings.Builder
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if strings.ContainsAny(kv, "\r\n") {
			return "", fmt.Errorf("sbxrt: переменная %s содержит перевод строки — файл окружения sbx такого не передаёт", k)
		}
		for _, s := range secrets {
			if v == s.value {
				v = s.placeholder
			}
		}
		b.WriteString(k + "=" + v + "\n")
	}
	return b.String(), nil
}

// fingerprint — отпечаток всего, что sbx фиксирует при создании песочницы
// (шаблон, маунты, ресурсы, правила сети): сменилось — песочница создаётся
// заново, совпало — будится та же VM.
func fingerprint(parts ...[]string) string {
	h := sha256.New()
	for _, p := range parts {
		for _, s := range p {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// templateListed — есть ли образ в выводе `sbx template ls -q` (имя с тегом
// либо без него).
func templateListed(out, image string) bool {
	repo := image
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		repo = image[:i]
	}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && (line == image || line == repo) {
			return true
		}
	}
	return false
}

// listed — есть ли name среди строк вывода `sbx ls -q`.
func listed(out, name string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}
