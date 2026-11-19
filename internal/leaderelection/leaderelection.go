// Package leaderelection — leader-под для фоновых циклов шлюза при 2+
// репликах (карточка ha-control-plane, план п.3): coordination.k8s.io Lease
// с renewDeadline; фолбэк single-реплики — циклы в процессе, как сейчас
// (флаг --ha-leader-lease не задан → Holder не создаётся).
//
// Зависимостей сверх client-go не тянет (controller-runtime — тяжёлая
// опция): acquire/renew/takeover на голом Lease API. Переходы лидерства
// — колбэки OnAcquired/OnLost; тики фоновых циклов шлюза спрашивают
// IsLeader() и пропускают работу, пока реплика не лидер.
package leaderelection

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Опциональные тайминги по умолчанию: LeaseDuration 15с, renew 5с, тик 2с —
// фолловер берёт лидерство через leaseDuration после смерти лидера.
const (
	DefaultLeaseDuration = 15 * time.Second
	DefaultRenewDeadline = 5 * time.Second
	DefaultRetryPeriod   = 2 * time.Second
)

// Options — параметры Holder.
type Options struct {
	Client        kubernetes.Interface
	Namespace     string // namespace Lease (по умолчанию: serviceaccount-файл, env, "default")
	Name          string // имя Lease, например "agctl-leader"
	Identity      string // держатель (по умолчанию hostname+суффикс)
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
	// OnAcquired вызывается однократно при каждом переходе в лидеры (в
	// фоне, из цикла Holder'а); OnLost — при потере лидерства.
	OnAcquired func(ctx context.Context)
	OnLost     func()
}

// Holder — владелец Lease; IsLeader отвечает на вопрос «мне ли гонять
// фоновые циклы». Нулевой Holder (флаг не задан) — всегда лидер: фолбэк
// single-реплики.
type Holder struct {
	opts Options

	mu     sync.Mutex
	leader bool
	done   chan struct{}
}

// ClientFromKubeconfig — kubernetes.Interface из флага --k8s-kubeconfig
// (пусто = in-cluster credentials, как у k8srt).
func ClientFromKubeconfig(kubeconfig string) (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("leaderelection: kubernetes config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

// NamespaceFromEnvironment — namespace шлюза в кластере: serviceaccount-файл,
// затем env KUBERNETES_NAMESPACE, затем "default".
func NamespaceFromEnvironment() string {
	if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil && len(b) > 0 {
		return string(b)
	}
	if v := os.Getenv("KUBERNETES_NAMESPACE"); v != "" {
		return v
	}
	return "default"
}

// New запускает цикл acquire/renew и возвращает Holder.
func New(ctx context.Context, opts Options) *Holder {
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = DefaultLeaseDuration
	}
	if opts.RenewDeadline <= 0 {
		opts.RenewDeadline = DefaultRenewDeadline
	}
	if opts.RetryPeriod <= 0 {
		opts.RetryPeriod = DefaultRetryPeriod
	}
	if opts.Identity == "" {
		host, _ := os.Hostname()
		opts.Identity = fmt.Sprintf("%s-%x", host, time.Now().UnixNano())
	}
	if opts.Namespace == "" {
		opts.Namespace = NamespaceFromEnvironment()
	}
	h := &Holder{opts: opts, done: make(chan struct{})}
	go h.loop(ctx)
	return h
}

// IsLeader — гонять ли фоновые циклы этой реплике.
func (h *Holder) IsLeader() bool {
	if h == nil {
		return true // фолбэк single-реплики: lease не настроен
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.leader
}

// Close прекращает участие в выборах (лидерство теряется по истечении
// LeaseDuration — фолловер подхватит).
func (h *Holder) Close() {
	if h == nil {
		return
	}
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *Holder) setLeader(v bool) (changed bool) {
	h.mu.Lock()
	changed = h.leader != v
	h.leader = v
	h.mu.Unlock()
	return changed
}

func (h *Holder) loop(ctx context.Context) {
	t := time.NewTicker(h.opts.RetryPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case <-t.C:
			h.tick(ctx)
		}
	}
}

func (h *Holder) tick(ctx context.Context) {
	api := h.opts.Client.CoordinationV1().Leases(h.opts.Namespace)
	now := metav1.NewMicroTime(time.Now())
	lease, err := api.Get(ctx, h.opts.Name, metav1.GetOptions{})
	holder := ""
	if err == nil && lease.Spec.HolderIdentity != nil {
		holder = *lease.Spec.HolderIdentity
	}
	switch {
	case apierrors.IsNotFound(err):
		// Try-acquire: создаём Lease от себя; проигравший гонку получает
		// AlreadyExists и ждёт истечения чужого leases.
		desired := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: h.opts.Name, Namespace: h.opts.Namespace},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &h.opts.Identity,
				LeaseDurationSeconds: int32Ptr(int32(h.opts.LeaseDuration.Seconds())),
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}
		if _, err := api.Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			log.Printf("[leaderelection] %s/%s: create: %v", h.opts.Namespace, h.opts.Name, err)
			return
		}
		if h.becomeLeader(ctx) {
			return
		}
	case err != nil:
		// Недоступный API: держим прошлое состояние до renewDeadline —
		// мигание лидерства из-за транзиента хуже краткой паузы.
		log.Printf("[leaderelection] %s/%s: get: %v", h.opts.Namespace, h.opts.Name, err)
		return
	case holder == h.opts.Identity:
		// Renew: CAS через resourceVersion; конфликт — кто-то отобрал, разбирается.
		lease.Spec.RenewTime = &now
		lease.Spec.AcquireTime = &now
		lease.Spec.LeaseDurationSeconds = int32Ptr(int32(h.opts.LeaseDuration.Seconds()))
		if _, err := api.Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
			if h.setLeader(false) && h.opts.OnLost != nil {
				h.opts.OnLost()
			}
			log.Printf("[leaderelection] %s/%s: renew: %v — лидерство потеряно", h.opts.Namespace, h.opts.Name, err)
		}
		return
	default:
		// Чужой держатель (или пустой Lease): берём лидерство только если
		// держателя нет или его lease истёк (renewTime + duration < now) —
		// стандартная семантика k8s leases.
		held := time.Duration(h.opts.LeaseDuration)
		if lease.Spec.LeaseDurationSeconds != nil && *lease.Spec.LeaseDurationSeconds > 0 {
			held = time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
		}
		expired := lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" ||
			lease.Spec.RenewTime == nil || lease.Spec.RenewTime.Add(held).Before(time.Now())
		if !expired {
			if h.setLeader(false) && h.opts.OnLost != nil {
				h.opts.OnLost()
			}
			return
		}
		lease.Spec.HolderIdentity = &h.opts.Identity
		lease.Spec.AcquireTime = &now
		lease.Spec.RenewTime = &now
		lease.Spec.LeaseDurationSeconds = int32Ptr(int32(h.opts.LeaseDuration.Seconds()))
		if _, err := api.Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
			// Конфликт resourceVersion — чужой takeover был раньше;retry на
			// следующем тике.
			log.Printf("[leaderelection] %s/%s: takeover: %v", h.opts.Namespace, h.opts.Name, err)
			return
		}
		if h.becomeLeader(ctx) {
			return
		}
	}
}

// becomeLeader — переход в лидеры: флаг + колбэк; false, если уже был лидером.
func (h *Holder) becomeLeader(ctx context.Context) bool {
	if !h.setLeader(true) {
		return false
	}
	log.Printf("[leaderelection] %s/%s: лидер — %s", h.opts.Namespace, h.opts.Name, h.opts.Identity)
	if h.opts.OnAcquired != nil {
		go h.opts.OnAcquired(ctx)
	}
	return true
}

func int32Ptr(v int32) *int32 { return &v }
