// Package axrt — адаптер runtime.ChatProvider поверх Google AX
// (github.com/google/ax) и Agent Substrate (github.com/agent-substrate/
// substrate): персональный dsh пользователя — это AX Task (актор Substrate в
// gVisor/microVM-песочнице). Сон по TTL = SuspendTask (снимок актора, состояние
// /workspace сохраняется), пробуждение = ResumeTask; трафик шлюза идёт через
// маршрутизатор Substrate (atenet-router) с заголовком ate-target-actor.
//
// Контракт образа: PID 1 в песочнице — /usr/local/bin/ax-task-runner, в
// образе agctl-dsh это обёртка над `agctl ax-runner` (cmd/agctl/axrunner.go):
// /healthz и /readyz на :80, реверс-прокси на dsh web :3080, хвост лога
// запуска (boot-токен dsh) на /agctl/boot-log по токену раннера.
package axrt

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// HeaderTargetActor — заголовок маршрутизатора Substrate: <atespace>/<actor>.
	HeaderTargetActor = "ate-target-actor"
	// BootLogPath — путь раннера с хвостом лога запуска dsh (boot-токен).
	BootLogPath = "/agctl/boot-log"
	// RunnerTokenEnv — env раннера с токеном доступа к BootLogPath.
	RunnerTokenEnv = "AGCTL_RUNNER_TOKEN"

	configKeySpecHash = "agctlSpecHash"
	configKeyVersion  = "agctlVersion"
	configKeyUserID   = "agctlUserId"
	configKeyRuntime  = "agctlRuntimeId"

	defaultReadyTimeout = 180 * time.Second
	pollInterval        = 2 * time.Second
)

// Config — настройки адаптера AX.
type Config struct {
	// Server — адрес gRPC ax-server (host:port), без TLS (как у `ax` в кластере).
	Server string
	// Atespace — atespace задач agctl (одна установка = один atespace).
	Atespace string
	// Image — образ песочницы agctl-dsh, обязательно pinned by digest
	// (name@sha256:…): Substrate отвергает теги.
	Image string
	// RouterURL — базовый URL маршрутизатора Substrate (atenet-router):
	// http://atenet-router.ate-system.svc.cluster.local:80 или https://…:443
	// (mTLS: RouterClientCert/Key + RouterCA).
	RouterURL        string
	RouterCA         string
	RouterClientCert string
	RouterClientKey  string
	// RunnerToken — секрет доступа к /agctl/boot-log раннера; передаётся задаче
	// через env (фиксируется при создании задачи).
	RunnerToken string
	// Version — версия сборки agctl (пишется в spec.config задачи).
	Version string
	// ReadyTimeout — ожидание Ready=True после resume (по умолчанию 180 с).
	ReadyTimeout time.Duration
	// InjectLLMKey — класть ключи провайдеров в env задачи. false = ключи в
	// песочницу не попадают: env получает placeholder, а реальный ключ
	// подменяет egress-шлюз Substrate (atenet-egress, replaceHeaders
	// Authorization). Egress != nil — agctl сам ведёт EgressPolicy актора и
	// Secret с ключами (egress.go); nil — политику настраивает оператор.
	InjectLLMKey bool
	// Egress — автоматизация egress-инъекции при InjectLLMKey=false; nil =
	// ручная политика оператора (docs/kubernetes.md).
	Egress *EgressAutomation
	// RecreateOnDrift — задача AX неизменяема; при смене образа/провайдера
	// пересоздать её (данные /workspace актора теряются!). false = оставить
	// старую задачу и предупредить в логе.
	RecreateOnDrift bool
	// ProviderAtBoot — не класть конфиг провайдера LLM в env задачи: раннер
	// забирает его у шлюза (GatewayURL) при каждом boot по токену раннера.
	// Провайдер выпадает из specHash — смена профиля/ключа не дрейфует,
	// задача и /workspace актора не пересоздаются.
	ProviderAtBoot bool
	// GatewayURL — базовый URL шлюза agctl для fetch-at-boot раннера
	// (требуется при ProviderAtBoot).
	GatewayURL string
	// CPU/Memory — лимиты песочницы (quantity: "2", "4Gi").
	CPU    string
	Memory string
}

// Provider — AX-реализация runtime.ChatProvider.
type Provider struct {
	cfg    Config
	ax     v1alpha1.AXClient
	router *http.Client
	conn   *grpc.ClientConn
	warned sync.Map
	specs  sync.Map // task name → ChatSpec (fetch-at-boot раннера)
}

// New — gRPC-клиент к ax-server и HTTP-клиент к маршрутизатору.
func New(cfg Config) (*Provider, error) {
	if cfg.Server == "" {
		return nil, fmt.Errorf("axrt: ax-server address is required")
	}
	conn, err := grpc.NewClient(cfg.Server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("axrt: connect ax-server %s: %w", cfg.Server, err)
	}
	p, err := NewWithClient(cfg, v1alpha1.NewAXClient(conn), nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	p.conn = conn
	return p, nil
}

// NewWithClient — инъекция клиентов (тесты). router nil = клиент по
// умолчанию с TLS из конфигурации.
func NewWithClient(cfg Config, ax v1alpha1.AXClient, router *http.Client) (*Provider, error) {
	if ax == nil {
		return nil, fmt.Errorf("axrt: AX client is required")
	}
	if cfg.Atespace == "" {
		cfg.Atespace = "agctl"
	}
	if cfg.Image == "" {
		return nil, fmt.Errorf("axrt: sandbox image is required")
	}
	if !strings.Contains(cfg.Image, "@sha256:") {
		return nil, fmt.Errorf("axrt: sandbox image %q must be pinned by digest (name@sha256:…)", cfg.Image)
	}
	if cfg.RouterURL == "" {
		cfg.RouterURL = "http://atenet-router.ate-system.svc.cluster.local:80"
	}
	if u, err := url.Parse(cfg.RouterURL); err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("axrt: invalid router URL %q", cfg.RouterURL)
	}
	if cfg.ProviderAtBoot && cfg.GatewayURL == "" {
		return nil, fmt.Errorf("axrt: provider-at-boot requires gateway URL")
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = defaultReadyTimeout
	}
	if cfg.CPU == "" {
		cfg.CPU = "2"
	}
	if cfg.Memory == "" {
		cfg.Memory = "2Gi"
	}
	if router == nil {
		var err error
		if router, err = routerClient(cfg); err != nil {
			return nil, err
		}
	}
	return &Provider{cfg: cfg, ax: ax, router: router}, nil
}

// routerClient — HTTP-клиент маршрутизатора (опционально mTLS).
func routerClient(cfg Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.RouterCA != "" || cfg.RouterClientCert != "" {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.RouterCA != "" {
			pem, err := os.ReadFile(cfg.RouterCA)
			if err != nil {
				return nil, fmt.Errorf("axrt: router CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("axrt: router CA %s: no certificates", cfg.RouterCA)
			}
			tlsCfg.RootCAs = pool
		}
		if cfg.RouterClientCert != "" {
			cert, err := tls.LoadX509KeyPair(cfg.RouterClientCert, cfg.RouterClientKey)
			if err != nil {
				return nil, fmt.Errorf("axrt: router client certificate: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
		transport.TLSClientConfig = tlsCfg
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

// Close закрывает gRPC-соединение.
func (p *Provider) Close() error {
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// Atespace — atespace установки.
func (p *Provider) Atespace() string { return p.cfg.Atespace }

// TaskName — имя задачи AX по RuntimeID (lowercase RFC 1123, ≤63).
func TaskName(runtimeID string) string {
	name := "agctl-" + strings.ToLower(runtimeID)
	clean := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, name)
	if len(clean) > 63 {
		sum := sha256.Sum256([]byte(runtimeID))
		clean = clean[:46] + "-" + hex.EncodeToString(sum[:8])
	}
	return strings.Trim(clean, "-")
}

// EnsureChat — задача пользователя существует и Running/Ready: GetTask →
// CreateTask (нет) → ResumeTask (не Running) → ожидание Ready. Endpoint —
// маршрутизатор, актор выбирается заголовком ate-target-actor.
func (p *Provider) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	if spec.RuntimeID == "" || spec.StorageID == "" {
		return agctlrt.ChatHandle{}, fmt.Errorf("axrt: runtime and storage IDs are required")
	}
	name := TaskName(spec.RuntimeID)
	if p.cfg.Egress != nil && !p.cfg.InjectLLMKey {
		if err := p.cfg.Egress.EnsureEgressPolicy(ctx, p.cfg.Atespace, name, spec); err != nil {
			return agctlrt.ChatHandle{}, err
		}
	}
	if p.cfg.ProviderAtBoot {
		p.specs.Store(name, spec)
	}
	desired := p.buildTask(name, spec)
	task, err := p.ax.GetTask(ctx, &v1alpha1.GetTaskRequest{Atespace: p.cfg.Atespace, Name: name})
	switch {
	case isNotFound(err):
		if task, err = p.ax.CreateTask(ctx, &v1alpha1.CreateTaskRequest{Task: desired}); err != nil {
			return agctlrt.ChatHandle{}, fmt.Errorf("axrt: create task %s: %w", name, err)
		}
	case err != nil:
		return agctlrt.ChatHandle{}, fmt.Errorf("axrt: get task %s: %w", name, err)
	default:
		if specHash(task) != specHash(desired) {
			if p.cfg.RecreateOnDrift {
				log.Printf("[axrt] задача %s: образ/провайдер изменились — пересоздаю (данные /workspace актора теряются)", name)
				if _, err := p.ax.DeleteTask(ctx, &v1alpha1.DeleteTaskRequest{Atespace: p.cfg.Atespace, Name: name}); err != nil && !isNotFound(err) {
					return agctlrt.ChatHandle{}, fmt.Errorf("axrt: delete task %s: %w", name, err)
				}
				if task, err = p.ax.CreateTask(ctx, &v1alpha1.CreateTaskRequest{Task: desired}); err != nil {
					return agctlrt.ChatHandle{}, fmt.Errorf("axrt: recreate task %s: %w", name, err)
				}
			} else if _, seen := p.warned.LoadOrStore(name, true); !seen {
				log.Printf("[axrt] задача %s: образ/провайдер изменились, но задача AX неизменяема — работает прежняя конфигурация (пересоздание: --ax-recreate-on-drift или удаление задачи админом)", name)
			}
		}
	}
	phase := task.GetStatus().GetPhase()
	if phase == v1alpha1.PhaseFailed {
		return agctlrt.ChatHandle{}, fmt.Errorf("axrt: task %s failed: %s", name, readyMessage(task))
	}
	if phase != v1alpha1.PhaseRunning && phase != v1alpha1.PhaseResuming {
		if _, err := p.ax.ResumeTask(ctx, &v1alpha1.ResumeTaskRequest{Atespace: p.cfg.Atespace, Name: name}); err != nil && !isAlreadyRunning(err) {
			return agctlrt.ChatHandle{}, fmt.Errorf("axrt: resume task %s: %w", name, err)
		}
	}
	task, err = p.waitReady(ctx, name)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	return agctlrt.ChatHandle{
		RuntimeID:  spec.RuntimeID,
		InstanceID: instanceID(task),
		Endpoint:   p.cfg.RouterURL,
		Headers:    map[string]string{HeaderTargetActor: p.cfg.Atespace + "/" + name},
	}, nil
}

// instanceID — <task>/<id>@<workerIP>: смена воркера после resume — новое
// поколение (новый boot-токен dsh).
func instanceID(task *v1alpha1.Task) string {
	st := task.GetStatus()
	return task.GetMetadata().GetName() + "/" + st.GetId() + "@" + st.GetWorkerIp()
}

// taskNameOf — имя задачи из InstanceID или имя как есть.
func taskNameOf(id string) string {
	name, _, _ := strings.Cut(id, "/")
	return name
}

// buildTask — желаемая спецификация задачи пользователя.
func (p *Provider) buildTask(name string, spec agctlrt.ChatSpec) *v1alpha1.Task {
	modelsJSON, _ := json.Marshal(spec.Provider.AllModels())
	env := map[string]string{
		"DSH_HOME":         "/workspace/dsh-home",
		"HOME":             "/workspace/work",
		"AGCTL_WS_ID":      spec.RuntimeID,
		"NPM_CONFIG_CACHE": "/tmp/npm-cache",
		"XDG_CACHE_HOME":   "/tmp/cache",
	}
	if p.cfg.ProviderAtBoot && p.cfg.GatewayURL != "" {
		// Конфиг провайдера — не в spec задачи: раннер заберёт его у шлюза
		// при каждом boot (GET /ax/provider-config/<task> по токену раннера),
		// смена профиля/ключа не меняет specHash.
		env["AGCTL_GATEWAY_URL"] = p.cfg.GatewayURL
	} else {
		env["AGCTL_LLM_PROVIDER"] = spec.Provider.Alias()
		env["AGCTL_LLM_BASE_URL"] = spec.Provider.BaseURL
		env["AGCTL_LLM_MODEL"] = spec.Provider.Model
		env["AGCTL_LLM_MODELS"] = string(modelsJSON)
		env["AGCTL_REFRESH_PROVIDER"] = "1"
	}
	if !p.cfg.ProviderAtBoot && spec.Provider.APIKey != "" {
		switch {
		case p.cfg.InjectLLMKey:
			env["AGCTL_LLM_API_KEY"] = spec.Provider.APIKey
		case p.cfg.Egress != nil:
			// egress-инъекция: placeholder в env, реальный ключ подставит
			// atenet-egress по EgressPolicy актора (egress.go)
			env["AGCTL_LLM_API_KEY"] = PlaceholderAPIKey
		}
	}
	type extraSpec struct {
		Name      string
		BaseURL   string
		Model     string
		Models    []string
		APIKeyEnv string
	}
	var extras []extraSpec
	for _, extra := range spec.Provider.Extras {
		if len(extra.AllModels()) == 0 || strings.TrimSpace(extra.BaseURL) == "" {
			continue
		}
		extras = append(extras, extraSpec{Name: extra.Name, BaseURL: extra.BaseURL, Model: extra.DefaultModel(), Models: extra.AllModels(), APIKeyEnv: extra.KeyEnv()})
		if !p.cfg.ProviderAtBoot && extra.APIKey != "" {
			switch {
			case p.cfg.InjectLLMKey:
				env[extra.KeyEnv()] = extra.APIKey
			case p.cfg.Egress != nil:
				env[extra.KeyEnv()] = PlaceholderAPIKey
			}
		}
	}
	if len(extras) > 0 && !p.cfg.ProviderAtBoot {
		if raw, err := json.Marshal(extras); err == nil {
			env["AGCTL_LLM_EXTRA_PROVIDERS"] = string(raw)
		}
	}
	if spec.DefaultFullAccess {
		env["AGCTL_DEFAULT_FULL_ACCESS"] = "1"
	}
	if p.cfg.RunnerToken != "" {
		env[RunnerTokenEnv] = p.cfg.RunnerToken
	}
	for _, kv := range spec.DevEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.TrimSpace(k) != "" {
			env[strings.TrimSpace(k)] = v
		}
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	envList := make([]*v1alpha1.EnvVar, 0, len(keys))
	hasher := sha256.New()
	image := p.cfg.Image
	if spec.Image != "" {
		image = spec.Image
	}
	hasher.Write([]byte(image + "\n"))
	for _, k := range keys {
		envList = append(envList, &v1alpha1.EnvVar{Name: k, Value: env[k]})
		hasher.Write([]byte(k + "=" + env[k] + "\n"))
	}
	config, _ := structpb.NewStruct(map[string]any{
		configKeySpecHash: hex.EncodeToString(hasher.Sum(nil)[:16]),
		configKeyVersion:  p.cfg.Version,
		configKeyUserID:   strconv.FormatInt(spec.UserID, 10),
		configKeyRuntime:  spec.RuntimeID,
	})
	return &v1alpha1.Task{
		ApiVersion: "ax.io/v1alpha1",
		Kind:       "Task",
		Metadata:   &v1alpha1.ObjectMeta{Name: name, Atespace: p.cfg.Atespace},
		Spec: &v1alpha1.TaskSpec{
			Image:   image,
			Command: []string{"/usr/local/bin/agctl-entrypoint"},
			Env:     envList,
			Resources: &v1alpha1.ResourcesSpec{Limits: &v1alpha1.ResourceList{
				Cpu: p.cfg.CPU, Memory: p.cfg.Memory,
			}},
			Debug:  false,
			Config: config,
		},
	}
}

// specHash — хеш конфигурации из spec.config задачи.
func specHash(task *v1alpha1.Task) string {
	fields := task.GetSpec().GetConfig().GetFields()
	if fields == nil {
		return ""
	}
	return fields[configKeySpecHash].GetStringValue()
}

// waitReady — ожидание Ready=True (ReadyTimeout); Failed — ошибка с причиной.
func (p *Provider) waitReady(ctx context.Context, name string) (*v1alpha1.Task, error) {
	deadline := time.Now().Add(p.cfg.ReadyTimeout)
	for {
		task, err := p.ax.GetTask(ctx, &v1alpha1.GetTaskRequest{Atespace: p.cfg.Atespace, Name: name})
		if err != nil {
			return nil, fmt.Errorf("axrt: get task %s: %w", name, err)
		}
		if task.GetStatus().GetPhase() == v1alpha1.PhaseFailed {
			return nil, fmt.Errorf("axrt: task %s failed: %s", name, readyMessage(task))
		}
		if taskReady(task) {
			return task, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("axrt: task %s not ready within %s (%s: %s)", name, p.cfg.ReadyTimeout, task.GetStatus().GetPhase(), readyMessage(task))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// taskReady — условие Ready=True (или Running без условий — старый сервер).
func taskReady(task *v1alpha1.Task) bool {
	st := task.GetStatus()
	for _, c := range st.GetConditions() {
		if c.GetType() == v1alpha1.ConditionReady {
			return c.GetStatus() == "True"
		}
	}
	return st.GetPhase() == v1alpha1.PhaseRunning && len(st.GetConditions()) == 0
}

// readyMessage — причина/сообщение условия Ready для ошибок.
func readyMessage(task *v1alpha1.Task) string {
	for _, c := range task.GetStatus().GetConditions() {
		if c.GetType() == v1alpha1.ConditionReady {
			return strings.TrimSpace(c.GetReason() + " " + c.GetMessage())
		}
	}
	return task.GetStatus().GetPhase()
}

// StopChat — SuspendTask: снимок актора, worker освобождается, состояние
// /workspace сохраняется до следующего resume. Задача не удаляется.
func (p *Provider) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	name := TaskName(handle.RuntimeID)
	_, err := p.ax.SuspendTask(ctx, &v1alpha1.SuspendTaskRequest{Atespace: p.cfg.Atespace, Name: name})
	if err != nil && !isNotFound(err) && !isAlreadySuspended(err) {
		return fmt.Errorf("axrt: suspend task %s: %w", name, err)
	}
	return nil
}

// LogsChat — хвост лога запуска dsh из раннера песочницы (через маршрутизатор,
// по токену раннера): источник boot-токена для mint cookie.
func (p *Provider) LogsChat(ctx context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	return p.bootLog(ctx, TaskName(handle.RuntimeID), tail)
}

func (p *Provider) bootLog(ctx context.Context, name, tail string) (string, error) {
	if p.cfg.RunnerToken == "" {
		return "", fmt.Errorf("axrt: runner token is not configured; boot log is unavailable")
	}
	u := strings.TrimRight(p.cfg.RouterURL, "/") + BootLogPath
	if n, err := strconv.Atoi(tail); err == nil && n > 0 {
		u += "?tail=" + strconv.Itoa(n)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(HeaderTargetActor, p.cfg.Atespace+"/"+name)
	req.Header.Set("Authorization", "Bearer "+p.cfg.RunnerToken)
	resp, err := p.router.Do(req)
	if err != nil {
		return "", fmt.Errorf("axrt: boot log %s: %w", name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("axrt: boot log %s: HTTP %d", name, resp.StatusCode)
	}
	return string(body), nil
}

// HasActiveSSH — SSH-канала в песочнице AX нет (sshd не публикуется).
func (p *Provider) HasActiveSSH(context.Context, agctlrt.ChatHandle) (bool, error) { return false, nil }

// HealthStatus — "unhealthy" для удалённых/упавших задач; иначе "healthy".
func (p *Provider) HealthStatus(ctx context.Context, instanceID string) (string, error) {
	task, err := p.ax.GetTask(ctx, &v1alpha1.GetTaskRequest{Atespace: p.cfg.Atespace, Name: taskNameOf(instanceID)})
	if isNotFound(err) {
		return "unhealthy", nil
	}
	if err != nil {
		return "", err
	}
	switch task.GetStatus().GetPhase() {
	case v1alpha1.PhaseFailed, v1alpha1.PhaseTerminating:
		return "unhealthy", nil
	}
	return "healthy", nil
}

// ListTasks — все задачи atespace (админка).
func (p *Provider) ListTasks(ctx context.Context) ([]*v1alpha1.Task, error) {
	var out []*v1alpha1.Task
	const page = 200
	for offset := int64(0); ; offset += page {
		resp, err := p.ax.ListTasks(ctx, &v1alpha1.ListTasksRequest{Atespace: p.cfg.Atespace, Limit: page, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("axrt: list tasks: %w", err)
		}
		out = append(out, resp.GetTasks()...)
		if len(resp.GetTasks()) < page {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetMetadata().GetName() < out[j].GetMetadata().GetName() })
	return out, nil
}

// DeleteTask — удалить задачу пользователя целиком (данные актора теряются);
// только для админского пересоздания.
func (p *Provider) DeleteTask(ctx context.Context, name string) error {
	_, err := p.ax.DeleteTask(ctx, &v1alpha1.DeleteTaskRequest{Atespace: p.cfg.Atespace, Name: name})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("axrt: delete task %s: %w", name, err)
	}
	return nil
}

func isNotFound(err error) bool {
	return err != nil && status.Code(err) == codes.NotFound
}

// isAlreadyRunning — resume уже запущенной задачи (FailedPrecondition/
// AlreadyExists у разных версий ax-server) — не ошибка.
func isAlreadyRunning(err error) bool {
	if err == nil {
		return false
	}
	code := status.Code(err)
	msg := strings.ToLower(err.Error())
	return (code == codes.FailedPrecondition || code == codes.AlreadyExists) && strings.Contains(msg, "running")
}

// isAlreadySuspended — suspend уже спящей задачи — не ошибка.
func isAlreadySuspended(err error) bool {
	if err == nil {
		return false
	}
	code := status.Code(err)
	msg := strings.ToLower(err.Error())
	return (code == codes.FailedPrecondition || code == codes.AlreadyExists) && strings.Contains(msg, "suspend")
}

// ErrLegacyUnsupported — legacy workspace-рантайм недоступен в AX.
var ErrLegacyUnsupported = errors.New("axrt: legacy workspace runtime is not supported on AX")
