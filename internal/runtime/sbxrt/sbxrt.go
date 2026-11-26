// Package sbxrt — рантайм рабочего места локального режима на Docker
// Sandboxes (CLI sbx): microVM на рабочее место вместо контейнера на общем
// ядре. Тот же контракт runtime.ChatProvider, что у dockerrt; локального API
// у sbx нет, поэтому всё управление — вызовами CLI.
//
// Использованные команды и флаги — по справочнику
// https://docs.docker.com/reference/cli/sbx/ (репозиторий docker/docs,
// data/sbx_cli, снимок 2026-11-25):
//
//	sbx version
//	sbx ls -q                                     имена песочниц
//	sbx template ls -q · sbx template load FILE   хранилище шаблонов sbx
//	sbx create --name N --template IMG --pull never [--cpus C] [--memory Mm]
//	           shell PRIMARY [HOST:TARGET...]     песочница без встроенного агента
//	sbx policy allow network --sandbox N RES,...  сеть (по умолчанию запрещена)
//	sbx secret set-custom --sandbox N --name S --host H --placeholder P --value-stdin
//	sbx port publish N 127.0.0.1:HOST:3080 · sbx port unpublish N 3080
//	sbx exec -u root -w /workspace --env-file F N agctl-entrypoint
//	sbx stop N · sbx rm --force N
//
// Глобальный --no-input: sbx не задаёт вопросов, а завершается ошибкой.
package sbxrt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
)

// InstancePrefix — префикс ChatHandle.InstanceID песочниц sbx: по нему
// Switch отличает их от контейнеров Docker.
const InstancePrefix = "sbx:"

// Config — настройки рантайма.
type Config struct {
	Bin         string // исполняемый файл sbx; пусто — "sbx" из PATH
	Home        string // дом данных сервера (chats/<StorageID>)
	Image       string // образ рабочего места по умолчанию
	MemoryBytes int64
	NanoCPUs    int64
	DshSkel     fs.FS
	// Allow — белый список сети сверх провайдеров модели и URL из окружения
	// рабочего места: хосты, IP, CIDR (egress.allow профиля, dev.yaml).
	Allow []string
	// ReadyTimeout — сколько ждать ответа dsh web после запуска; 0 = 3 минуты.
	ReadyTimeout time.Duration
	// ImageID и SaveImage — доступ к локальному Docker, из которого образ
	// выгружается в шаблон sbx; nil — dockerrt.
	ImageID   func(ctx context.Context, ref string) (string, error)
	SaveImage func(ctx context.Context, ref, path string) error
}

// Runtime — реализация runtime.ChatProvider поверх CLI sbx.
type Runtime struct {
	cfg   Config
	mu    sync.Mutex
	procs map[string]*proc // имя песочницы → живой `sbx exec` с dsh web
}

// New проверяет, что sbx установлен и готов, и возвращает рантайм.
func New(ctx context.Context, cfg Config) (*Runtime, error) {
	if cfg.Bin == "" {
		cfg.Bin = "sbx"
	}
	if cfg.Home == "" {
		return nil, errors.New("sbxrt: не задан дом данных")
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 3 * time.Minute
	}
	if cfg.ImageID == nil {
		cfg.ImageID = dockerrt.ImageID
	}
	if cfg.SaveImage == nil {
		cfg.SaveImage = dockerrt.SaveImage
	}
	if _, err := Probe(ctx, cfg.Bin); err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, procs: map[string]*proc{}}, nil
}

// Probe — установлен ли sbx и готов ли он создавать песочницы на этой машине:
// бинарь в PATH, платформа с виртуализацией, демон отвечает и вход выполнен
// (`sbx ls` без вопросов). Возвращает строку версии.
func Probe(ctx context.Context, bin string) (string, error) {
	if bin == "" {
		bin = "sbx"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", errors.New("sbx не установлен — https://docs.docker.com/ai/sandboxes/install/ (macOS: brew trust docker/tap && brew install docker/tap/sbx), затем sbx login")
	}
	if err := hostSupport(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	version, _, err := run(ctx, path, "", "version")
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(strings.SplitN(strings.TrimSpace(version), "\n", 2)[0])
	if _, _, err := run(ctx, path, "", "--no-input", "ls", "-q"); err != nil {
		return version, fmt.Errorf("sbx установлен (%s), но не готов — выполните sbx login и проверьте sbx diagnose: %w", version, err)
	}
	return version, nil
}

// hostSupport — требования локальных песочниц к платформе (docs: install):
// macOS — только Apple Silicon; Linux — KVM и доступ к /dev/kvm; Windows —
// Windows Hypervisor Platform (отсюда не проверить — ошибку даст сам sbx).
var hostSupport = func() error {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH != "arm64" {
			return errors.New("sbx: на macOS локальные песочницы работают только на Apple Silicon")
		}
	case "linux":
		f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("sbx: нет доступа к /dev/kvm (нужны KVM и группа kvm: sudo usermod -aG kvm $USER): %w", err)
		}
		_ = f.Close()
	}
	return nil
}

// run выполняет CLI и возвращает stdout и stderr раздельно. В тексте ошибки —
// только подкоманда и хвост вывода: значения доступов в аргументы не
// передаются, но и пути с именами лишний раз в лог не тянем.
func run(ctx context.Context, bin, stdin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		verb := args
		for len(verb) > 0 && strings.HasPrefix(verb[0], "-") {
			verb = verb[1:]
		}
		if len(verb) > 2 {
			verb = verb[:2]
		}
		for i, a := range verb {
			if strings.HasPrefix(a, "-") {
				verb = verb[:i]
				break
			}
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), stderr.String(), fmt.Errorf("sbx %s: %w: %s", strings.Join(verb, " "), err, lastLines(msg, 6))
	}
	return stdout.String(), stderr.String(), nil
}

func (rt *Runtime) sbx(ctx context.Context, stdin string, args ...string) (string, error) {
	out, _, err := run(ctx, rt.cfg.Bin, stdin, append([]string{"--no-input"}, args...)...)
	return out, err
}

// sandboxState — что agctl помнит о созданной песочнице (chats/<id>/sbx-state.json).
type sandboxState struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

// EnsureChat поднимает рабочее место в песочнице sbx: дом dsh и папки
// проектов — прямые маунты с хоста (как bind в Docker), dsh web публикуется
// на 127.0.0.1:<свободный порт>. Песочница с прежними параметрами будится,
// с изменившимися (образ, маунты, ресурсы, правила сети) — создаётся заново;
// данные живут на хосте и не теряются.
func (rt *Runtime) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	fail := func(err error) (agctlrt.ChatHandle, error) { return agctlrt.ChatHandle{}, err }
	if spec.StorageID == "" || filepath.Base(spec.StorageID) != spec.StorageID {
		return fail(fmt.Errorf("sbxrt: invalid chat storage id %q", spec.StorageID))
	}
	base := filepath.Join(rt.cfg.Home, "chats", spec.StorageID)
	dshHome, work, primary := filepath.Join(base, "dsh-home"), filepath.Join(base, "work"), filepath.Join(base, "sbx")
	if spec.DshHome != "" && filepath.IsAbs(spec.DshHome) {
		dshHome = spec.DshHome
	}
	if spec.WorkDir != "" {
		if len(spec.Mounts) > 0 {
			return fail(errors.New("sbxrt: WorkDir and Mounts are mutually exclusive"))
		}
		if err := checkDir(spec.WorkDir); err != nil {
			return fail(err)
		}
		work = spec.WorkDir
	}
	mounts := []string{dshHome + ":/home/dsh", work + ":/workspace"}
	seen := map[string]bool{}
	for _, m := range spec.Mounts {
		if err := checkDir(m.Host); err != nil {
			return fail(err)
		}
		if !validMountName(m.Name) || seen[m.Name] {
			return fail(fmt.Errorf("sbxrt: invalid or duplicate mount name %q", m.Name))
		}
		seen[m.Name] = true
		mounts = append(mounts, m.Host+":/workspace/"+m.Name)
	}
	for _, dir := range []string{dshHome, work, primary} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(err)
		}
	}
	if spec.SSHPubKey != "" {
		log.Printf("[sbxrt] %s: SSH-канал в рантайме sbx не поддерживается — ключ не передан, порт не публикуется", spec.RuntimeID)
	}
	name := SandboxName(spec.RuntimeID)
	// Ключи моделей в VM не попадают: в .credentials.yaml и окружении —
	// заглушки, настоящие значения подставляет прокси sbx на хосте.
	provider, secrets := substituteKeys(name, spec.Provider)
	if err := rt.prepareDshHome(dshHome, spec, provider); err != nil {
		return fail(err)
	}
	env := append([]string{
		"AGCTL_WS_ID=" + spec.RuntimeID,
		"DSH_HOME=/home/dsh",
		"HOME=/workspace",
		"NPM_CONFIG_CACHE=/tmp/npm-cache",
		"XDG_CACHE_HOME=/tmp/cache",
	}, spec.DevEnv...)
	envFile, err := envFileContent(env, secrets)
	if err != nil {
		return fail(err)
	}

	image := spec.Image
	if image == "" {
		image = rt.cfg.Image
	}
	imageID, err := rt.ensureTemplate(ctx, image)
	if err != nil {
		return fail(err)
	}
	allow := networkResources(spec.Provider, spec.DevEnv, rt.cfg.Allow)
	create := createArgs(name, image, primary, mounts, rt.cfg.NanoCPUs, rt.cfg.MemoryBytes)
	fp := fingerprint(create, []string{imageID}, allow)

	rt.killProc(name)
	out, err := rt.sbx(ctx, "", "ls", "-q")
	if err != nil {
		return fail(err)
	}
	exists := listed(out, name)
	statePath := filepath.Join(base, "sbx-state.json")
	if exists && readState(statePath).Fingerprint != fp {
		log.Printf("[sbxrt] %s: параметры песочницы изменились — создаю заново (данные на маунтах хоста не тронуты)", name)
		if _, err := rt.sbx(ctx, "", "rm", "--force", name); err != nil {
			return fail(err)
		}
		exists = false
	}
	if exists {
		// Песочница могла пережить аварийное завершение agctl: гасим прежний
		// dsh и снимаем старую публикацию порта (ошибки «уже остановлена» /
		// «не опубликован» ожидаемы).
		_, _ = rt.sbx(ctx, "", "port", "unpublish", name, dshPort)
		_, _ = rt.sbx(ctx, "", "stop", name)
	} else {
		if _, err := rt.sbx(ctx, "", create...); err != nil {
			return fail(err)
		}
		if len(allow) > 0 {
			if _, err := rt.sbx(ctx, "", "policy", "allow", "network", "--sandbox", name, strings.Join(allow, ",")); err != nil {
				return fail(err)
			}
		}
		raw, _ := json.Marshal(sandboxState{Name: name, Fingerprint: fp})
		if err := os.WriteFile(statePath, raw, 0o600); err != nil {
			return fail(err)
		}
	}
	for _, s := range secrets {
		args := []string{"secret", "set-custom", "--sandbox", name, "--name", s.name, "--placeholder", s.placeholder, "--value-stdin"}
		for _, h := range s.hosts {
			args = append(args, "--host", h)
		}
		if _, err := rt.sbx(ctx, s.value, args...); err != nil {
			return fail(fmt.Errorf("sbxrt: ключ модели не передан прокси sbx (в VM он не попадает, без подстановки модель не ответит): %w", err))
		}
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		return fail(err)
	}
	// Публикация порта будит остановленную песочницу (docs: port publish).
	if _, err := rt.sbx(ctx, "", "port", "publish", name, "127.0.0.1:"+strconv.Itoa(port)+":"+dshPort); err != nil {
		return fail(err)
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	p, err := rt.startDsh(name, envFile)
	if err != nil {
		return fail(err)
	}
	if err := rt.waitReady(ctx, p, endpoint); err != nil {
		rt.killProc(name)
		_, _ = rt.sbx(context.WithoutCancel(ctx), "", "stop", name)
		return fail(err)
	}
	return agctlrt.ChatHandle{RuntimeID: spec.RuntimeID, InstanceID: InstancePrefix + name, Endpoint: endpoint}, nil
}

const dshPort = "3080"

// prepareDshHome — тот же посев и освежение дома dsh, что делает dockerrt
// перед стартом контейнера.
func (rt *Runtime) prepareDshHome(dshHome string, spec agctlrt.ChatSpec, provider provision.Provider) error {
	if _, err := os.Stat(filepath.Join(dshHome, "profiles", "web", "package.json")); err != nil {
		if rt.cfg.DshSkel == nil {
			return errors.New("sbxrt: no embedded DSH skeleton for first chat boot")
		}
		if err := provision.SeedDshHome(dshHome, provider, rt.cfg.DshSkel); err != nil {
			return err
		}
	}
	if err := provision.RefreshProvider(dshHome, provider); err != nil {
		return err
	}
	ensurePlugins := provision.EnsureChatPlugins
	if spec.Local {
		ensurePlugins = provision.EnsureChatPluginsLocal
	}
	if err := ensurePlugins(dshHome, rt.cfg.DshSkel); err != nil {
		return err
	}
	if err := provision.EnsurePermissionDefaults(dshHome, spec.DefaultFullAccess); err != nil {
		return fmt.Errorf("sbxrt: permission defaults: %w", err)
	}
	return nil
}

// ensureTemplate — образ рабочего места в хранилище шаблонов sbx. У sbx своё
// хранилище образов, отдельное от Docker хоста: образ выгружается из Docker в
// tar и загружается `sbx template load`. Повторно — только когда образ в
// Docker сменился (пересборка под тем же тегом) или шаблона нет.
func (rt *Runtime) ensureTemplate(ctx context.Context, image string) (string, error) {
	if image == "" {
		return "", errors.New("sbxrt: не задан образ рабочего места")
	}
	id, err := rt.cfg.ImageID(ctx, image)
	if err != nil {
		return "", fmt.Errorf("sbxrt: образа %s нет в локальном Docker (оттуда он выгружается в шаблон sbx): %w", image, err)
	}
	known := map[string]string{}
	knownPath := filepath.Join(rt.cfg.Home, "sbx-templates.json")
	if raw, err := os.ReadFile(knownPath); err == nil {
		_ = json.Unmarshal(raw, &known)
	}
	out, err := rt.sbx(ctx, "", "template", "ls", "-q")
	if err != nil {
		return "", err
	}
	if known[image] == id && templateListed(out, image) {
		return id, nil
	}
	log.Printf("[sbxrt] загружаю образ %s в хранилище шаблонов sbx (docker save → sbx template load)", image)
	tmp, err := os.CreateTemp(rt.cfg.Home, "sbx-template-*.tar")
	if err != nil {
		return "", err
	}
	_ = tmp.Close()
	defer os.Remove(tmp.Name())
	if err := rt.cfg.SaveImage(ctx, image, tmp.Name()); err != nil {
		return "", err
	}
	if _, err := rt.sbx(ctx, "", "template", "load", tmp.Name()); err != nil {
		return "", err
	}
	known[image] = id
	raw, _ := json.Marshal(known)
	if err := os.WriteFile(knownPath, raw, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// StopChat усыпляет песочницу (sbx stop): VM и всё установленное в ней
// сохраняются до следующего EnsureChat, данные — на маунтах хоста.
func (rt *Runtime) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	name := strings.TrimPrefix(handle.InstanceID, InstancePrefix)
	if name == "" {
		return nil
	}
	rt.killProc(name)
	out, err := rt.sbx(ctx, "", "ls", "-q")
	if err != nil {
		return err
	}
	if !listed(out, name) {
		return nil
	}
	// Опубликованный порт sbx восстановил бы при следующем старте — снимаем.
	_, _ = rt.sbx(ctx, "", "port", "unpublish", name, dshPort)
	_, err = rt.sbx(ctx, "", "stop", name)
	return err
}

// Remove удаляет песочницу целиком (VM и её содержимое; маунты хоста остаются).
func (rt *Runtime) Remove(ctx context.Context, handle agctlrt.ChatHandle) error {
	name := strings.TrimPrefix(handle.InstanceID, InstancePrefix)
	rt.killProc(name)
	_, err := rt.sbx(ctx, "", "rm", "--force", name)
	return err
}

// LogsChat — хвост вывода dsh web (источник boot-токена dsh).
func (rt *Runtime) LogsChat(_ context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	name := strings.TrimPrefix(handle.InstanceID, InstancePrefix)
	rt.mu.Lock()
	p := rt.procs[name]
	rt.mu.Unlock()
	if p == nil {
		return "", fmt.Errorf("sbxrt: рабочее место %s не запущено", name)
	}
	n, err := strconv.Atoi(tail)
	if err != nil || n <= 0 {
		return p.log.String(), nil
	}
	return lastLines(p.log.String(), n), nil
}

// HasActiveSSH — SSH-канала в sbx нет.
func (rt *Runtime) HasActiveSSH(context.Context, agctlrt.ChatHandle) (bool, error) {
	return false, nil
}

// HealthStatus — "unhealthy", когда процесс dsh web в песочнице завершился
// (сервер тогда пересоздаст рабочее место при следующем открытии).
func (rt *Runtime) HealthStatus(_ context.Context, instanceID string) (string, error) {
	rt.mu.Lock()
	p := rt.procs[strings.TrimPrefix(instanceID, InstancePrefix)]
	rt.mu.Unlock()
	if p == nil || p.exited() {
		return "unhealthy", nil
	}
	return "healthy", nil
}

// Close завершает процессы dsh web (песочницы останавливает StopChat).
func (rt *Runtime) Close() error {
	rt.mu.Lock()
	names := make([]string, 0, len(rt.procs))
	for name := range rt.procs {
		names = append(names, name)
	}
	rt.mu.Unlock()
	for _, name := range names {
		rt.killProc(name)
	}
	return nil
}

// ── процесс dsh web ─────────────────────────────────────────────────────────

// proc — `sbx exec … agctl-entrypoint`: sbx держит песочницу сам и команду
// образа не запускает, а exec не умеет отсоединяться — dsh web живёт, пока
// жив этот дочерний процесс agctl.
type proc struct {
	cmd  *exec.Cmd
	log  *logBuf
	done chan struct{}
}

func (p *proc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (rt *Runtime) startDsh(name, envFile string) (*proc, error) {
	f, err := os.CreateTemp("", "agctl-sbx-env-*")
	if err != nil {
		return nil, err
	}
	envPath := f.Name()
	_, werr := f.WriteString(envFile)
	if cerr := f.Close(); werr != nil || cerr != nil {
		_ = os.Remove(envPath)
		return nil, errors.Join(werr, cerr)
	}
	p := &proc{log: &logBuf{}, done: make(chan struct{})}
	// Запуск от root в /workspace — как контейнер Docker с тем же образом.
	p.cmd = exec.Command(rt.cfg.Bin, "--no-input", "exec", "-u", "root", "-w", "/workspace", "--env-file", envPath, name, "agctl-entrypoint")
	p.cmd.Stdout, p.cmd.Stderr = p.log, p.log
	if err := p.cmd.Start(); err != nil {
		_ = os.Remove(envPath)
		return nil, fmt.Errorf("sbx exec: %w", err)
	}
	go func() {
		_ = p.cmd.Wait()
		close(p.done)
	}()
	// Файл окружения sbx читает при старте exec; дольше он на диске не нужен.
	go func() {
		select {
		case <-p.done:
		case <-time.After(time.Minute):
		}
		_ = os.Remove(envPath)
	}()
	rt.mu.Lock()
	rt.procs[name] = p
	rt.mu.Unlock()
	return p, nil
}

func (rt *Runtime) killProc(name string) {
	rt.mu.Lock()
	p := rt.procs[name]
	delete(rt.procs, name)
	rt.mu.Unlock()
	if p == nil || p.exited() {
		return
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
	}
}

// waitReady ждёт HTTP-ответа dsh web на опубликованном порту. Завершение
// процесса или таймаут — ошибка с хвостом вывода, а не «поднято, но молчит».
func (rt *Runtime) waitReady(ctx context.Context, p *proc, endpoint string) error {
	deadline := time.Now().Add(rt.cfg.ReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		if resp, err := client.Get(endpoint + "/"); err == nil {
			_ = resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return fmt.Errorf("sbxrt: dsh web в песочнице завершился при старте:\n%s", lastLines(p.log.String(), 15))
		case <-time.After(300 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sbxrt: dsh web не ответил на %s за %s:\n%s", endpoint, rt.cfg.ReadyTimeout, lastLines(p.log.String(), 15))
		}
	}
}

// logBuf — хвост вывода процесса (не более 256 КБ).
type logBuf struct {
	mu sync.Mutex
	b  []byte
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b = append(l.b, p...)
	if over := len(l.b) - 256<<10; over > 0 {
		l.b = append([]byte(nil), l.b[over:]...)
	}
	return len(p), nil
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.b)
}

// ── мелочи ─────────────────────────────────────────────────────────────────

func readState(path string) sandboxState {
	var st sandboxState
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func checkDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("sbxrt: path must be absolute: %q", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("sbxrt: %q is not a directory", dir)
	}
	return nil
}

func validMountName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || (i > 0 && (r == '.' || r == '_' || r == '-'))) {
			return false
		}
	}
	return true
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
