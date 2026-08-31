package account

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsSeedanceFast(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"", true},
		{"doubao-seedance-2-0-fast", true},
		{"doubao-seedance-2-0-mini", false},
		{"seedance-mini", false},
		{"anything-else", true},
	}
	for _, tc := range cases {
		if got := IsSeedanceFast(tc.model); got != tc.want {
			t.Fatalf("IsSeedanceFast(%q)=%v want %v", tc.model, got, tc.want)
		}
	}
}

func TestQuotaCost(t *testing.T) {
	if got := QuotaCost("doubao-seedance-2-0-mini"); got != 1 {
		t.Fatalf("mini cost=%d want 1", got)
	}
	if got := QuotaCost("doubao-seedance-2-0-fast"); got != 2 {
		t.Fatalf("fast cost=%d want 2", got)
	}
	if got := QuotaCost(""); got != 2 {
		t.Fatalf("default fast cost=%d want 2", got)
	}
}

func TestLegacyGenCountToUnits(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 2: 3, 3: 5, 4: 5, 5: 5}
	for in, want := range cases {
		if got := legacyGenCountToUnits(in); got != want {
			t.Fatalf("legacyGenCountToUnits(%d)=%d want %d", in, got, want)
		}
	}
}

func TestMigrateQuotaUnitScale(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "accounts.db")
	store, err := Open(dbPath, filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate pre-migration DB: clear scale flag and set legacy remainings.
	store.mu.Lock()
	_, err = store.db.Exec(`DELETE FROM meta WHERE key=?`, metaQuotaUnitScaleKey)
	if err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	_, err = store.db.Exec(`DELETE FROM accounts`)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = Open(dbPath, filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("full", "./s/full", 3)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("one", "./s/one", 1)
	if err != nil {
		t.Fatal(err)
	}
	a3, err := store.Create("zero", "./s/zero", 0)
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	_, err = store.db.Exec(`DELETE FROM meta WHERE key=?`, metaQuotaUnitScaleKey)
	if err == nil {
		err = store.migrateQuotaUnitScaleUnlocked()
	}
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	got1, _ := store.Get(a1.ID)
	got2, _ := store.Get(a2.ID)
	got3, _ := store.Get(a3.ID)
	if got1.FastRemaining != 5 || got2.FastRemaining != 1 || got3.FastRemaining != 0 {
		t.Fatalf("after migrate: full=%d one=%d zero=%d", got1.FastRemaining, got2.FastRemaining, got3.FastRemaining)
	}

	// Second run is a no-op.
	store.mu.Lock()
	err = store.migrateQuotaUnitScaleUnlocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	got1, _ = store.Get(a1.ID)
	if got1.FastRemaining != 5 {
		t.Fatalf("second migrate changed remaining to %d", got1.FastRemaining)
	}
}

func TestInitQuotaDayDoesNotWipeRemaining(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create("ljy", "./session/liu", 1)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate upgrade / missing quota_day after manual config.
	store.mu.Lock()
	_, err = store.db.Exec(`DELETE FROM meta WHERE key=?`, metaQuotaDayKey)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := store.EnsureDailyReset(); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FastRemaining != 1 {
		t.Fatalf("init quota_day wiped remaining: got %d want 1", got.FastRemaining)
	}

	sw, err := store.ConsumeFastOnSuccess("doubao-seedance-2-0-mini")
	if err != nil {
		t.Fatal(err)
	}
	if sw.RemainingAfter != 0 {
		t.Fatalf("after consume remaining=%d want 0 (got switch=%+v)", sw.RemainingAfter, sw)
	}
}

func TestDailyQuotaResetBeijing(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create("alice", "./sessions/alice", 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeFastOnSuccess("doubao-seedance-2-0-mini"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FastRemaining != 4 {
		t.Fatalf("remaining=%d want 4", got.FastRemaining)
	}

	// Pretend last quota day was yesterday (Beijing).
	yesterday := beijingDate(time.Now().Add(-24 * time.Hour))
	store.mu.Lock()
	_, err = store.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, metaQuotaDayKey, yesterday)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].FastRemaining != DefaultFastRemaining {
		t.Fatalf("after daily reset: %+v", list)
	}
}

func TestConsumeFastAndAutoSwitch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "accounts.db")
	sessionFile := filepath.Join(dir, "active_session")
	store, err := Open(dbPath, sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 1)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	active, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a1.ID {
		t.Fatalf("active=%d want %d", active.ID, a1.ID)
	}

	sw, err := store.ConsumeFastOnSuccess("doubao-seedance-2-0-mini")
	if err != nil {
		t.Fatal(err)
	}
	if !sw.Switched || sw.ToID != a2.ID {
		t.Fatalf("switch=%+v want switch to %d", sw, a2.ID)
	}

	data, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "./sessions/bob\n" {
		t.Fatalf("active_session=%q", got)
	}

	active, err = store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a2.ID || active.FastRemaining != 5 {
		t.Fatalf("active after switch: %+v", active)
	}

	a1b, err := store.Get(a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a1b.FastRemaining != 0 {
		t.Fatalf("alice remaining=%d want 0", a1b.FastRemaining)
	}
}

func TestEnsureActiveHasQuotaSwitchesBeforeGenerate(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	sw, err := store.EnsureActiveHasQuota()
	if err != nil {
		t.Fatal(err)
	}
	if !sw.Switched || sw.ToID != a2.ID {
		t.Fatalf("switch=%+v want switch to %d (from exhausted %d)", sw, a2.ID, a1.ID)
	}

	active, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a2.ID || active.FastRemaining != 5 {
		t.Fatalf("active=%+v", active)
	}

	// Already has quota: no switch.
	sw2, err := store.EnsureActiveHasQuota()
	if err != nil {
		t.Fatal(err)
	}
	if sw2.Switched || sw2.RemainingAfter != 5 {
		t.Fatalf("unexpected: %+v", sw2)
	}
}

func TestEnsureActiveHasQuotaNoAlternate(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create("alice", "./sessions/alice", 0); err != nil {
		t.Fatal(err)
	}
	sw, err := store.EnsureActiveHasQuota()
	if !errors.Is(err, ErrNoQuotaAvailable) {
		t.Fatalf("err=%v sw=%+v want ErrNoQuotaAvailable", err, sw)
	}
	if sw.Switched {
		t.Fatalf("unexpected switch: %+v", sw)
	}
}

func TestMarkExhaustedAndSwitch(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 2)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	sw, err := store.MarkExhaustedAndSwitch()
	if err != nil {
		t.Fatal(err)
	}
	if !sw.Switched || sw.ToID != a2.ID || sw.RemainingAfter != 0 {
		t.Fatalf("switch=%+v want switch to %d remaining=0", sw, a2.ID)
	}

	a1b, err := store.Get(a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a1b.FastRemaining != 0 {
		t.Fatalf("alice remaining=%d want 0", a1b.FastRemaining)
	}

	active, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a2.ID || active.FastRemaining != 5 {
		t.Fatalf("active after mark: %+v", active)
	}
}

func TestMarkExhaustedNoAlternate(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 2)
	if err != nil {
		t.Fatal(err)
	}

	sw, err := store.MarkExhaustedAndSwitch()
	if err != nil {
		t.Fatal(err)
	}
	if sw.Switched {
		t.Fatalf("unexpected switch: %+v", sw)
	}

	got, err := store.Get(a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FastRemaining != 0 {
		t.Fatalf("remaining=%d want 0", got.FastRemaining)
	}
}

func TestAcquireReleaseLease(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 5)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	l1, err := store.AcquireForVideo()
	if err != nil {
		t.Fatal(err)
	}
	if l1.Account.ID != a1.ID {
		t.Fatalf("lease1=%d want %d", l1.Account.ID, a1.ID)
	}
	l2, err := store.AcquireForVideo()
	if err != nil {
		t.Fatal(err)
	}
	if l2.Account.ID != a2.ID {
		t.Fatalf("lease2=%d want %d", l2.Account.ID, a2.ID)
	}
	_, err = store.AcquireForVideo()
	if !errors.Is(err, ErrAllLeased) {
		t.Fatalf("err=%v want ErrAllLeased", err)
	}

	store.Release(a1.ID)
	l3, err := store.AcquireForVideo()
	if err != nil {
		t.Fatal(err)
	}
	if l3.Account.ID != a1.ID {
		t.Fatalf("lease3=%d want %d", l3.Account.ID, a1.ID)
	}
	store.Release(a1.ID)
	store.Release(a2.ID)
}

func TestAcquireNoQuota(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Create("alice", "./sessions/alice", 0); err != nil {
		t.Fatal(err)
	}
	_, err = store.AcquireForVideo()
	if !errors.Is(err, ErrNoQuotaAvailable) {
		t.Fatalf("err=%v want ErrNoQuotaAvailable", err)
	}
}

func TestAcquireFromAllowed(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 5)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	allowed := map[int64]struct{}{a2.ID: {}}
	l, err := store.AcquireForVideoFrom(allowed)
	if err != nil {
		t.Fatal(err)
	}
	if l.Account.ID != a2.ID {
		t.Fatalf("got %d want %d (alice=%d not in pool)", l.Account.ID, a2.ID, a1.ID)
	}
	store.Release(a2.ID)
}

func TestConsumeQuotaByModel(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a, err := store.Create("alice", "./sessions/alice", DefaultFastRemaining)
	if err != nil {
		t.Fatal(err)
	}

	// Fast: 5 -> 3 -> 1 -> 0 (three gens even though unit cost is 2).
	for i, want := range []int{3, 1, 0} {
		sw, err := store.ConsumeOnSuccess(a.ID, "doubao-seedance-2-0-fast")
		if err != nil {
			t.Fatal(err)
		}
		if sw.RemainingAfter != want {
			t.Fatalf("fast gen %d: remaining=%d want %d", i+1, sw.RemainingAfter, want)
		}
	}

	b, err := store.Create("bob", "./sessions/bob", DefaultFastRemaining)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := store.ConsumeOnSuccess(b.ID, "doubao-seedance-2-0-mini")
	if err != nil {
		t.Fatal(err)
	}
	if sw.RemainingAfter != 4 {
		t.Fatalf("mini remaining=%d want 4", sw.RemainingAfter)
	}

	// Remaining 1: Fast still allowed, clamps cost to 1.
	c, err := store.Create("carol", "./sessions/carol", 1)
	if err != nil {
		t.Fatal(err)
	}
	sw, err = store.ConsumeOnSuccess(c.ID, "doubao-seedance-2-0-fast")
	if err != nil {
		t.Fatal(err)
	}
	if sw.RemainingAfter != 0 {
		t.Fatalf("fast with remaining=1: got %d want 0", sw.RemainingAfter)
	}
}

func TestConsumeOnSuccessAndMarkExhausted(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Create("alice", "./sessions/alice", 3)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Create("bob", "./sessions/bob", 5)
	if err != nil {
		t.Fatal(err)
	}

	sw, err := store.ConsumeOnSuccess(a1.ID, "doubao-seedance-2-0-mini")
	if err != nil {
		t.Fatal(err)
	}
	if sw.RemainingAfter != 2 || sw.Switched {
		t.Fatalf("unexpected: %+v", sw)
	}
	// Active should still be alice (first created / selected by Create).
	active, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a1.ID {
		t.Fatalf("active switched unexpectedly to %d", active.ID)
	}

	sw, err = store.MarkExhausted(a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sw.RemainingAfter != 0 {
		t.Fatalf("unexpected: %+v", sw)
	}
	got, err := store.Get(a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FastRemaining != 0 {
		t.Fatalf("bob remaining=%d want 0", got.FastRemaining)
	}
	active, err = store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != a1.ID {
		t.Fatalf("active changed after MarkExhausted: %d", active.ID)
	}
}

func TestGenerationHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "accounts.db"), filepath.Join(dir, "active_session"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	g1, err := store.AddGeneration(GenerationInput{
		Kind:        GenerationKindImage,
		Prompt:      "一只猫",
		Model:       "doubao-seedream-5-0",
		Images:      []string{"/admin/history/media/a.png"},
		AccountID:   5,
		AccountName: "ma",
	})
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID == 0 || g1.Prompt != "一只猫" || len(g1.Images) != 1 || g1.AccountName != "ma" {
		t.Fatalf("unexpected generation: %+v", g1)
	}

	_, err = store.AddGeneration(GenerationInput{
		Kind:        GenerationKindVideo,
		Prompt:      "跳舞",
		ResultURL:   "https://example.com/v.mp4",
		TaskID:      "task-1",
		Images:      []string{"/admin/history/media/ref.png"},
		AccountID:   2,
		AccountName: "ljy",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := store.ListGenerations(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d want 2", len(list))
	}
	if list[0].Kind != GenerationKindVideo || list[0].AccountName != "ljy" {
		t.Fatalf("order/account wrong: %+v", list[0])
	}
	if list[1].Kind != GenerationKindImage || list[1].AccountName != "ma" {
		t.Fatalf("order/account wrong: %+v", list[1])
	}

	total, err := store.CountGenerations()
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("count=%d want 2", total)
	}
	page1, err := store.ListGenerationsPage(0, 1)
	if err != nil || len(page1) != 1 || page1[0].Kind != GenerationKindVideo {
		t.Fatalf("page1=%v err=%v", page1, err)
	}
	page2, err := store.ListGenerationsPage(1, 1)
	if err != nil || len(page2) != 1 || page2[0].Kind != GenerationKindImage {
		t.Fatalf("page2=%v err=%v", page2, err)
	}
}
