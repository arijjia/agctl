package leaderelection

// leaderelection_test.go — поведение выборов на fake clientset (карточка
// ha-control-plane): первый acquire, блокировка чужим живым lease, takeover
// по истечении, потеря при конфликте renew, фолбэк nil-Holder.

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newHolder(t *testing.T, client *fake.Clientset, identity string, acquired *chan struct{}) *Holder {
	t.Helper()
	opts := Options{
		Client:        client,
		Namespace:     "agctl",
		Name:          "agctl-leader",
		Identity:      identity,
		LeaseDuration: 2 * time.Second,
		RenewDeadline: time.Second,
		RetryPeriod:   150 * time.Millisecond,
	}
	if acquired != nil {
		opts.OnAcquired = func(ctx context.Context) { close(*acquired) }
	}
	return New(context.Background(), opts)
}

func waitLeader(t *testing.T, h *Holder, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.IsLeader() == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("IsLeader = %v, ждём %v", h.IsLeader(), want)
}

func TestNilHolderAlwaysLeader(t *testing.T) {
	var h *Holder
	if !h.IsLeader() {
		t.Fatal("nil-Holder (фолбэк single-реплики) должен быть всегда лидером")
	}
	h.Close() // не паникует
}

func TestFirstAcquireAndRenew(t *testing.T) {
	client := fake.NewSimpleClientset()
	acquired := make(chan struct{})
	h := newHolder(t, client, "replica-a", &acquired)
	defer h.Close()

	select {
	case <-acquired:
	case <-time.After(3 * time.Second):
		t.Fatal("первый acquire не случился")
	}
	if !h.IsLeader() {
		t.Fatal("после acquire IsLeader=false")
	}
	// Lease существует и обновляется: renewTime движется.
	api := client.CoordinationV1().Leases("agctl")
	first, err := api.Get(context.Background(), "agctl-leader", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	second, err := api.Get(context.Background(), "agctl-leader", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Spec.RenewTime.After(first.Spec.RenewTime.Time) {
		t.Fatal("renewTime не обновляется")
	}
}

func TestOtherHolderBlocksUntilExpiry(t *testing.T) {
	// Чужой ЖИВОЙ lease: наш холдер не лидер.
	fresh := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "agctl-leader", Namespace: "agctl"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       strPtr("replica-a"),
			LeaseDurationSeconds: int32Ptr(60), // долго не истекает
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	client := fake.NewSimpleClientset(fresh)
	h := newHolder(t, client, "replica-b", nil)
	defer h.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.IsLeader() {
			t.Fatal("взяли лидерство у живого держателя")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Истечение: renewTime уводим в прошлое за пределы LeaseDuration —
	// холдер делает takeover.
	api := client.CoordinationV1().Leases("agctl")
	lease, _ := api.Get(context.Background(), "agctl-leader", metav1.GetOptions{})
	lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now().Add(-2 * time.Minute)}
	if _, err := api.Update(context.Background(), lease, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitLeader(t, h, true, 3*time.Second)
	// Держатель сменился.
	got, _ := api.Get(context.Background(), "agctl-leader", metav1.GetOptions{})
	if *got.Spec.HolderIdentity != "replica-b" {
		t.Fatalf("держатель: %s", *got.Spec.HolderIdentity)
	}
}

func TestLeaderLosesOnRenewConflict(t *testing.T) {
	client := fake.NewSimpleClientset()
	acquired := make(chan struct{})
	h := newHolder(t, client, "replica-a", &acquired)
	defer h.Close()
	select {
	case <-acquired:
	case <-time.After(3 * time.Second):
		t.Fatal("acquire не случился")
	}
	waitLeader(t, h, true, time.Second)

	// Подменяем lease под ним (как будто отобрали/пересоздали): renew даёт
	// конфликт → лидерство теряется.
	api := client.CoordinationV1().Leases("agctl")
	lease, _ := api.Get(context.Background(), "agctl-leader", metav1.GetOptions{})
	lease.Spec.HolderIdentity = strPtr("replica-z")
	if _, err := api.Update(context.Background(), lease, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitLeader(t, h, false, 3*time.Second)
}

func TestHolderIdentityMissingLease(t *testing.T) {
	// Lease без держателя (мусорный объект) — берём как expired.
	empty := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "agctl-leader", Namespace: "agctl"},
	}
	client := fake.NewSimpleClientset(empty)
	h := newHolder(t, client, "replica-a", nil)
	defer h.Close()
	waitLeader(t, h, true, 3*time.Second)
	if _, err := client.CoordinationV1().Leases("agctl").Get(context.Background(), "agctl-leader", metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}
	}
}

func strPtr(s string) *string { return &s }
