package pool

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/account"
)

func newTestPool(t *testing.T, store *account.Store, ids ...int64) *Pool {
	t.Helper()
	p := &Pool{
		accounts: store,
		cfg:      Config{MaxParallel: 2, BaseCDPPort: 9222},
		notify:   make(chan struct{}, 1),
	}
	for _, id := range ids {
		a, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		p.workers = append(p.workers, &Worker{
			AccountID:  a.ID,
			Name:       a.Name,
			SessionDir: a.SessionDir,
			CDPPort:    9222 + len(p.workers),
		})
	}
	return p
}

func TestPoolAcquireTwoWorkersParallel(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 3)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 3)
	if err != nil {
		t.Fatal(err)
	}

	p := newTestPool(t, store, a1.ID, a2.ID)
	ctx := context.Background()

	w1, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if w1.AccountID == w2.AccountID {
		t.Fatalf("both workers same account %d", w1.AccountID)
	}

	done := make(chan error, 1)
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := p.Acquire(ctx2)
		done <- err
	}()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third acquire err=%v want deadline", err)
	}

	p.Release(w1)
	w3, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if w3.AccountID != w1.AccountID {
		t.Fatalf("got %d want released %d", w3.AccountID, w1.AccountID)
	}
	p.Release(w2)
	p.Release(w3)
}

func TestPoolAcquireNoQuota(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPool(t, store, a1.ID)
	_, err = p.Acquire(context.Background())
	if !errors.Is(err, account.ErrNoQuotaAvailable) {
		t.Fatalf("err=%v want ErrNoQuotaAvailable", err)
	}
}

func TestPoolRebindToAccountWithQuota(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Warm pool stuck on exhausted accounts; quota remains on xh-like account.
	// MaxParallel=1 so we rebind instead of scaling up a second Chrome.
	a0, err := store.Create("alice", "./sessions/alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	xh, err := store.Create("xh", "./sessions/xh", 3)
	if err != nil {
		t.Fatal(err)
	}

	p := newTestPool(t, store, a0.ID)
	p.cfg.MaxParallel = 1
	w, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.AccountID != xh.ID {
		t.Fatalf("rebound account=%d want %d (%s)", w.AccountID, xh.ID, w.Name)
	}
	if p.Len() != 1 {
		t.Fatalf("len=%d want 1 (rebind, not scale)", p.Len())
	}
	p.Release(w)
}

func TestPoolRebindWhenPeerBusyAndOutsideQuota(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Concurrent: alice + bob warm at MaxParallel=2; alice UI-exhausted while bob
	// still busy. Third account xh has quota — must rebind idle alice Chrome to xh
	// instead of waiting on ErrAllLeased.
	alice, err := store.Create("alice", "./sessions/alice", 3)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.Create("bob", "./sessions/bob", 3)
	if err != nil {
		t.Fatal(err)
	}
	xh, err := store.Create("xh", "./sessions/xh", 3)
	if err != nil {
		t.Fatal(err)
	}

	p := newTestPool(t, store, alice.ID, bob.ID)
	p.cfg.MaxParallel = 2

	wAlice, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wBob, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if wAlice.AccountID != alice.ID || wBob.AccountID != bob.ID {
		t.Fatalf("workers alice=%d bob=%d want %d/%d", wAlice.AccountID, wBob.AccountID, alice.ID, bob.ID)
	}

	if _, err := store.MarkExhausted(alice.ID); err != nil {
		t.Fatal(err)
	}
	p.Release(wAlice)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wRetry, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("expected rebind to xh while bob busy: %v", err)
	}
	if wRetry.AccountID != xh.ID {
		t.Fatalf("rebound account=%d (%s) want xh=%d", wRetry.AccountID, wRetry.Name, xh.ID)
	}
	if p.Len() != 2 {
		t.Fatalf("len=%d want 2 (rebind, not scale past max)", p.Len())
	}
	p.Release(wRetry)
	p.Release(wBob)
}

func TestPoolScaleUpOnConcurrentDemand(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("ba", "./sessions/ba", 3)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("ma", "./sessions/ma", 3)
	if err != nil {
		t.Fatal(err)
	}

	// Start with a single warm worker; second Acquire should scale up.
	p := newTestPool(t, store, a1.ID)
	if p.Len() != 1 {
		t.Fatalf("len=%d want 1", p.Len())
	}

	w1, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w2, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.Len() != 2 {
		t.Fatalf("len=%d want 2 after scale-up", p.Len())
	}
	if w1.AccountID == w2.AccountID {
		t.Fatalf("both same account %d", w1.AccountID)
	}
	ids := map[int64]bool{w1.AccountID: true, w2.AccountID: true}
	if !ids[a1.ID] || !ids[a2.ID] {
		t.Fatalf("workers=%d,%d want %d and %d", w1.AccountID, w2.AccountID, a1.ID, a2.ID)
	}
	p.Release(w1)
	p.Release(w2)
}

func TestPlanSlotsPrefersQuota(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create("zero", "./sessions/zero", 0); err != nil {
		t.Fatal(err)
	}
	ba, err := store.Create("ba", "./sessions/ba", 3)
	if err != nil {
		t.Fatal(err)
	}
	ma, err := store.Create("ma", "./sessions/ma", 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Select(ma.ID); err != nil {
		t.Fatal(err)
	}

	p := &Pool{
		accounts: store,
		cfg:      Config{MaxParallel: 2},
	}
	slots, err := p.planSlots(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 {
		t.Fatalf("len=%d want 2", len(slots))
	}
	ids := map[int64]bool{slots[0].AccountID: true, slots[1].AccountID: true}
	if !ids[ba.ID] || !ids[ma.ID] {
		t.Fatalf("slots=%+v want ba=%d ma=%d", slots, ba.ID, ma.ID)
	}

	initial, err := p.planInitialSlot()
	if err != nil {
		t.Fatal(err)
	}
	if initial.AccountID != ma.ID {
		t.Fatalf("initial=%+v want active ma=%d", initial, ma.ID)
	}
}

func TestPlanSlotsSkipsExhaustedActiveForDefaultWorker(t *testing.T) {
	dir := t.TempDir()
	store, err := account.Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exhausted, err := store.Create("exhausted", "./sessions/exhausted", 0)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.Create("ready", "./sessions/ready", 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Select(exhausted.ID); err != nil {
		t.Fatal(err)
	}

	p := &Pool{accounts: store, cfg: Config{MaxParallel: 2}}
	initial, err := p.planInitialSlot()
	if err != nil {
		t.Fatal(err)
	}
	if initial.AccountID != ready.ID {
		t.Fatalf("initial=%+v want quota-ready account=%d", initial, ready.ID)
	}
}
