package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultFastRemaining = 5
	QuotaCostMini        = 1
	QuotaCostFast        = 2
	metaQuotaDayKey      = "quota_day"
	// metaQuotaUnitScaleKey marks one-time conversion from legacy "gens left"
	// (cost 1 each, default 3) to unit remaining (Mini=1, Fast=2, default 5).
	metaQuotaUnitScaleKey = "quota_unit_scale"
	quotaUnitScaleV2      = "v2"
)

var (
	beijingLocation = mustLoadBeijing()

	ErrNotFound        = errors.New("account not found")
	ErrNameRequired    = errors.New("name is required")
	ErrSessionRequired = errors.New("session_dir is required")
	// ErrNoQuotaAvailable means every enabled account has local remaining == 0.
	ErrNoQuotaAvailable = errors.New("no account with remaining Seedance quota")
	// ErrAllLeased means every account with remaining quota is currently leased.
	ErrAllLeased = errors.New("all accounts with quota are currently leased")
	// ErrAlreadyLeased means the requested account is already leased.
	ErrAlreadyLeased = errors.New("account already leased")
)

func mustLoadBeijing() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// Fixed UTC+8 fallback if tzdata is unavailable.
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// Account is a Doubao Chrome session slot with local Seedance quota.
type Account struct {
	ID            int64
	Name          string
	SessionDir    string
	FastRemaining int
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SwitchResult describes an automatic or manual active-account change.
type SwitchResult struct {
	Switched       bool
	FromID         int64
	ToID           int64
	ToName         string
	ToSessionDir   string
	RemainingAfter int
	Message        string
}

// Lease is a process-local exclusive claim on an account for one video gen.
type Lease struct {
	Account Account
}

// Store persists accounts in SQLite and the active session path on disk.
type Store struct {
	db                *sql.DB
	activeSessionPath string
	mu                sync.Mutex
	leased            map[int64]struct{} // in-process video leases
	now               func() time.Time   // injectable for tests
}

// Open opens (or creates) the SQLite database and ensures schema.
func Open(dbPath, activeSessionPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = "./data/accounts.db"
	}
	if activeSessionPath == "" {
		activeSessionPath = "./data/active_session"
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(activeSessionPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir active session dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{
		db:                db,
		activeSessionPath: activeSessionPath,
		leased:            make(map[int64]struct{}),
		now:               time.Now,
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.mu.Lock()
	if err := s.migrateQuotaUnitScaleUnlocked(); err != nil {
		s.mu.Unlock()
		_ = db.Close()
		return nil, err
	}
	_ = s.ensureDailyResetUnlocked()
	s.mu.Unlock()
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	session_dir TEXT NOT NULL,
	fast_remaining INTEGER NOT NULL DEFAULT 5,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS generation_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL,
	prompt TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'succeeded',
	images_json TEXT NOT NULL DEFAULT '[]',
	result_url TEXT NOT NULL DEFAULT '',
	task_id TEXT NOT NULL DEFAULT '',
	account_id INTEGER NOT NULL DEFAULT 0,
	account_name TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	return s.ensureColumns(`generation_history`, map[string]string{
		"account_id":   `INTEGER NOT NULL DEFAULT 0`,
		"account_name": `TEXT NOT NULL DEFAULT ''`,
	})
}

// migrateQuotaUnitScaleUnlocked converts legacy per-generation remaining (default 3,
// Fast/Mini both cost 1) into unit remaining (default 5, Mini=1 Fast=2). Historical
// usage was Fast-dominated, so N gens left maps to 2N-1 units (last Fast can run on 1).
func (s *Store) migrateQuotaUnitScaleUnlocked() error {
	var scale string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, metaQuotaUnitScaleKey).Scan(&scale)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if scale == quotaUnitScaleV2 {
		return nil
	}

	rows, err := s.db.Query(`SELECT id, name, fast_remaining FROM accounts`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id   int64
		name string
		old  int
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.old); err != nil {
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	now := s.now().UTC().Format(time.RFC3339)
	for _, r := range list {
		// Values above the legacy default are already on the unit scale (or manual).
		if r.old > 3 {
			continue
		}
		next := legacyGenCountToUnits(r.old)
		if next == r.old {
			continue
		}
		if _, err := s.db.Exec(`UPDATE accounts SET fast_remaining=?, updated_at=? WHERE id=?`,
			next, now, r.id); err != nil {
			return err
		}
		log.Printf("account: quota scale migrate %s id=%d remaining %d -> %d", r.name, r.id, r.old, next)
	}

	if _, err := s.db.Exec(`
INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, metaQuotaUnitScaleKey, quotaUnitScaleV2); err != nil {
		return err
	}
	log.Printf("account: quota unit scale set to %s", quotaUnitScaleV2)
	return nil
}

// legacyGenCountToUnits maps old "generations remaining" to unit remaining for Fast.
// 3→5, 2→3, 1→1, 0→0 (so three Fast gens still fit in a full day).
func legacyGenCountToUnits(gens int) int {
	if gens <= 0 {
		return 0
	}
	units := 2*gens - 1
	if units > DefaultFastRemaining {
		return DefaultFastRemaining
	}
	return units
}

func (s *Store) ensureColumns(table string, cols map[string]string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name, def := range cols {
		if have[name] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + def); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) List() ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
SELECT id, name, session_dir, fast_remaining, enabled, created_at, updated_at
FROM accounts ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return Account{}, err
	}
	return s.getUnlocked(id)
}

func (s *Store) getUnlocked(id int64) (Account, error) {
	row := s.db.QueryRow(`
SELECT id, name, session_dir, fast_remaining, enabled, created_at, updated_at
FROM accounts WHERE id = ?`, id)
	a, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

func (s *Store) Create(name, sessionDir string, fastRemaining int) (Account, error) {
	name = strings.TrimSpace(name)
	sessionDir = strings.TrimSpace(sessionDir)
	if name == "" {
		return Account{}, ErrNameRequired
	}
	if sessionDir == "" {
		return Account{}, ErrSessionRequired
	}
	if fastRemaining < 0 {
		fastRemaining = DefaultFastRemaining
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
INSERT INTO accounts (name, session_dir, fast_remaining, enabled, created_at, updated_at)
VALUES (?, ?, ?, 1, ?, ?)`, name, sessionDir, fastRemaining, now, now)
	if err != nil {
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}

	activeID, _ := s.getActiveIDUnlocked()
	if activeID == 0 {
		if err := s.setActiveUnlocked(id); err != nil {
			return Account{}, err
		}
	}
	return s.getUnlocked(id)
}

type UpdateInput struct {
	Name          *string
	SessionDir    *string
	FastRemaining *int
	Enabled       *bool
}

func (s *Store) Update(id int64, in UpdateInput) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, err := s.getUnlocked(id)
	if err != nil {
		return Account{}, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return Account{}, ErrNameRequired
		}
		a.Name = name
	}
	if in.SessionDir != nil {
		dir := strings.TrimSpace(*in.SessionDir)
		if dir == "" {
			return Account{}, ErrSessionRequired
		}
		a.SessionDir = dir
	}
	if in.FastRemaining != nil {
		if *in.FastRemaining < 0 {
			return Account{}, fmt.Errorf("fast_remaining must be >= 0")
		}
		a.FastRemaining = *in.FastRemaining
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`
UPDATE accounts SET name=?, session_dir=?, fast_remaining=?, enabled=?, updated_at=?
WHERE id=?`, a.Name, a.SessionDir, a.FastRemaining, boolToInt(a.Enabled), now, id)
	if err != nil {
		return Account{}, err
	}
	activeID, _ := s.getActiveIDUnlocked()
	if activeID == id {
		_ = s.writeActiveSessionFile(a.SessionDir)
	}
	return s.getUnlocked(id)
}

func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.getUnlocked(id); err != nil {
		return err
	}
	activeID, _ := s.getActiveIDUnlocked()
	if _, err := s.db.Exec(`DELETE FROM accounts WHERE id=?`, id); err != nil {
		return err
	}
	if activeID != id {
		return nil
	}
	// Pick another account if available.
	var nextID sql.NullInt64
	_ = s.db.QueryRow(`SELECT id FROM accounts ORDER BY id ASC LIMIT 1`).Scan(&nextID)
	if nextID.Valid {
		return s.setActiveUnlocked(nextID.Int64)
	}
	_, _ = s.db.Exec(`DELETE FROM meta WHERE key='active_account_id'`)
	_ = os.Remove(s.activeSessionPath)
	return nil
}

func (s *Store) ResetFast(id int64, remaining int) (Account, error) {
	if remaining < 0 {
		remaining = DefaultFastRemaining
	}
	n := remaining
	return s.Update(id, UpdateInput{FastRemaining: &n})
}

// ActiveID returns the currently selected account id (0 if none).
func (s *Store) ActiveID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getActiveIDUnlocked()
}

// Active returns the active account, or ErrNotFound.
func (s *Store) Active() (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return Account{}, err
	}
	id, err := s.getActiveIDUnlocked()
	if err != nil {
		return Account{}, err
	}
	if id == 0 {
		return Account{}, ErrNotFound
	}
	return s.getUnlocked(id)
}

// Select makes id the active account and writes data/active_session.
func (s *Store) Select(id int64) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.getUnlocked(id)
	if err != nil {
		return Account{}, err
	}
	if err := s.setActiveUnlocked(id); err != nil {
		return Account{}, err
	}
	return a, nil
}

// EnsureActiveHasQuota switches away from an exhausted active account
// (fast_remaining <= 0) onto the next enabled account with remaining > 0.
// Call this before starting a video generation so local "3 times used up"
// triggers a session switch without waiting for the Doubao UI tip.
// Returns ErrNoQuotaAvailable when no enabled account has remaining quota.
func (s *Store) EnsureActiveHasQuota() (SwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return SwitchResult{}, err
	}

	activeID, err := s.getActiveIDUnlocked()
	if err != nil {
		return SwitchResult{}, err
	}
	if activeID == 0 {
		return SwitchResult{}, ErrNotFound
	}
	a, err := s.getUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}
	if a.FastRemaining > 0 {
		return SwitchResult{
			FromID:         activeID,
			RemainingAfter: a.FastRemaining,
		}, nil
	}

	sw, err := s.autoSwitchUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}
	sw.FromID = activeID
	sw.RemainingAfter = 0
	if !sw.Switched {
		return sw, ErrNoQuotaAvailable
	}
	return sw, nil
}

// AcquireForVideo leases an enabled account with remaining > 0 that is not
// already leased. Returns ErrNoQuotaAvailable or ErrAllLeased.
func (s *Store) AcquireForVideo() (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return Lease{}, err
	}
	return s.acquireForVideoUnlocked(nil)
}

// AcquireForVideoFrom leases an account whose id is in allowed (pool workers).
// If allowed is empty, any eligible account may be leased.
func (s *Store) AcquireForVideoFrom(allowed map[int64]struct{}) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return Lease{}, err
	}
	return s.acquireForVideoUnlocked(allowed)
}

func (s *Store) acquireForVideoUnlocked(allowed map[int64]struct{}) (Lease, error) {
	rows, err := s.db.Query(`
SELECT id, name, session_dir, fast_remaining, enabled, created_at, updated_at
FROM accounts
WHERE enabled=1 AND fast_remaining>0
ORDER BY id ASC`)
	if err != nil {
		return Lease{}, err
	}
	defer rows.Close()

	hasQuota := false
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return Lease{}, err
		}
		if allowed != nil {
			if _, ok := allowed[a.ID]; !ok {
				continue
			}
		}
		hasQuota = true
		if _, leased := s.leased[a.ID]; leased {
			continue
		}
		s.leased[a.ID] = struct{}{}
		return Lease{Account: a}, nil
	}
	if err := rows.Err(); err != nil {
		return Lease{}, err
	}
	if !hasQuota {
		return Lease{}, ErrNoQuotaAvailable
	}
	return Lease{}, ErrAllLeased
}

// TryLease leases a specific account if it has remaining quota and is free.
func (s *Store) TryLease(accountID int64) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return Lease{}, err
	}
	if _, ok := s.leased[accountID]; ok {
		return Lease{}, ErrAlreadyLeased
	}
	a, err := s.getUnlocked(accountID)
	if err != nil {
		return Lease{}, err
	}
	if !a.Enabled || a.FastRemaining <= 0 {
		return Lease{}, ErrNoQuotaAvailable
	}
	s.leased[accountID] = struct{}{}
	return Lease{Account: a}, nil
}

// Release drops an in-process video lease.
func (s *Store) Release(accountID int64) {
	if s == nil || accountID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leased, accountID)
}

// IsLeased reports whether accountID currently holds a video lease.
func (s *Store) IsLeased(accountID int64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.leased[accountID]
	return ok
}

// ConsumeOnSuccess decrements Seedance remaining for the given account by model cost
// (Mini=1, Fast=2; Fast with remaining=1 still consumes down to 0).
// Unlike ConsumeFastOnSuccess it does not auto-switch the global active account.
func (s *Store) ConsumeOnSuccess(accountID int64, model string) (SwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return SwitchResult{}, err
	}
	if accountID == 0 {
		return SwitchResult{Message: "no account"}, nil
	}
	a, err := s.getUnlocked(accountID)
	if err != nil {
		return SwitchResult{}, err
	}
	if a.FastRemaining <= 0 {
		return SwitchResult{
			FromID:         accountID,
			RemainingAfter: 0,
			Message:        fmt.Sprintf("account %s already remaining=0", a.Name),
		}, nil
	}
	cost := QuotaCost(model)
	if cost > a.FastRemaining {
		cost = a.FastRemaining
	}
	now := s.now().UTC().Format(time.RFC3339)
	newRemaining := a.FastRemaining - cost
	if _, err := s.db.Exec(`UPDATE accounts SET fast_remaining=?, updated_at=? WHERE id=?`,
		newRemaining, now, accountID); err != nil {
		return SwitchResult{}, err
	}
	return SwitchResult{
		FromID:         accountID,
		RemainingAfter: newRemaining,
		Message:        fmt.Sprintf("account %s remaining=%d (cost=%d)", a.Name, newRemaining, cost),
	}, nil
}

// MarkExhausted zeros remaining for the given account (Doubao UI quota tip).
// Does not switch the global active account.
func (s *Store) MarkExhausted(accountID int64) (SwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return SwitchResult{}, err
	}
	if accountID == 0 {
		return SwitchResult{Message: "no account"}, nil
	}
	a, err := s.getUnlocked(accountID)
	if err != nil {
		return SwitchResult{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE accounts SET fast_remaining=0, updated_at=? WHERE id=?`,
		now, accountID); err != nil {
		return SwitchResult{}, err
	}
	log.Printf("account: UI quota exhausted on %s (id=%d), remaining set to 0 (was %d)",
		a.Name, accountID, a.FastRemaining)
	return SwitchResult{
		FromID:         accountID,
		RemainingAfter: 0,
		Message:        fmt.Sprintf("account %s marked exhausted (remaining=0)", a.Name),
	}, nil
}

// ListEnabled returns enabled accounts ordered by id.
func (s *Store) ListEnabled() ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
SELECT id, name, session_dir, fast_remaining, enabled, created_at, updated_at
FROM accounts WHERE enabled=1 ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ConsumeFastOnSuccess decrements shared Seedance remaining for the active account
// when a video generation succeeds (Mini=1, Fast=2; Fast with remaining=1 still
// consumes down to 0). If remaining hits 0, switches to the next enabled account
// with remaining > 0.
func (s *Store) ConsumeFastOnSuccess(model string) (SwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return SwitchResult{}, err
	}

	activeID, err := s.getActiveIDUnlocked()
	if err != nil {
		return SwitchResult{}, err
	}
	if activeID == 0 {
		return SwitchResult{Message: "no active account"}, nil
	}
	a, err := s.getUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}
	if a.FastRemaining <= 0 {
		sw, err := s.autoSwitchUnlocked(activeID)
		if err != nil {
			return SwitchResult{}, err
		}
		sw.RemainingAfter = 0
		return sw, nil
	}

	cost := QuotaCost(model)
	if cost > a.FastRemaining {
		cost = a.FastRemaining
	}
	now := s.now().UTC().Format(time.RFC3339)
	newRemaining := a.FastRemaining - cost
	_, err = s.db.Exec(`UPDATE accounts SET fast_remaining=?, updated_at=? WHERE id=?`,
		newRemaining, now, activeID)
	if err != nil {
		return SwitchResult{}, err
	}

	res := SwitchResult{
		FromID:         activeID,
		RemainingAfter: newRemaining,
	}
	if newRemaining > 0 {
		res.Message = fmt.Sprintf("account %s remaining=%d (cost=%d)", a.Name, newRemaining, cost)
		return res, nil
	}

	sw, err := s.autoSwitchUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}
	sw.FromID = activeID
	sw.RemainingAfter = 0
	return sw, nil
}

// MarkExhaustedAndSwitch sets the active account's remaining to 0 (Doubao UI
// reported quota exhausted) and switches to the next enabled account with
// remaining > 0, if any.
func (s *Store) MarkExhaustedAndSwitch() (SwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDailyResetUnlocked(); err != nil {
		return SwitchResult{}, err
	}

	activeID, err := s.getActiveIDUnlocked()
	if err != nil {
		return SwitchResult{}, err
	}
	if activeID == 0 {
		return SwitchResult{Message: "no active account"}, nil
	}
	a, err := s.getUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}

	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE accounts SET fast_remaining=0, updated_at=? WHERE id=?`,
		now, activeID); err != nil {
		return SwitchResult{}, err
	}
	log.Printf("account: UI quota exhausted on %s (id=%d), remaining set to 0 (was %d)",
		a.Name, activeID, a.FastRemaining)

	sw, err := s.autoSwitchUnlocked(activeID)
	if err != nil {
		return SwitchResult{}, err
	}
	sw.FromID = activeID
	sw.RemainingAfter = 0
	if !sw.Switched && sw.Message == "" {
		sw.Message = fmt.Sprintf("account %s marked exhausted (remaining=0); no alternate account", a.Name)
	} else if !sw.Switched {
		sw.Message = fmt.Sprintf("account %s marked exhausted (remaining=0); %s", a.Name, sw.Message)
	}
	return sw, nil
}

// EnsureDailyReset resets all accounts' remaining quota when the Beijing calendar
// day advances past the last recorded quota_day. Safe to call concurrently.
func (s *Store) EnsureDailyReset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureDailyResetUnlocked()
}

// StartDailyResetLoop checks around Beijing midnight so quota resets even without traffic.
func (s *Store) StartDailyResetLoop(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.EnsureDailyReset(); err != nil {
					log.Printf("account: daily reset check: %v", err)
				}
			}
		}
	}()
}

func beijingDate(t time.Time) string {
	return t.In(beijingLocation).Format("2006-01-02")
}

func (s *Store) ensureDailyResetUnlocked() error {
	today := beijingDate(s.now())
	var stored string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, metaQuotaDayKey).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if stored == today {
		return nil
	}

	// First-time init: only record today's date. Do NOT wipe manually configured remaining.
	if stored == "" {
		if _, err := s.db.Exec(`
INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, metaQuotaDayKey, today); err != nil {
			return err
		}
		log.Printf("account: initialized quota_day=%s (Beijing), keeping existing remaining", today)
		return nil
	}

	now := s.now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE accounts SET fast_remaining=?, updated_at=?`, DefaultFastRemaining, now); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
INSERT INTO meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, metaQuotaDayKey, today); err != nil {
		return err
	}
	log.Printf("account: daily quota reset %s -> %s (Beijing), all accounts remaining=%d",
		stored, today, DefaultFastRemaining)
	return nil
}

func (s *Store) autoSwitchUnlocked(fromID int64) (SwitchResult, error) {
	row := s.db.QueryRow(`
SELECT id, name, session_dir, fast_remaining, enabled, created_at, updated_at
FROM accounts
WHERE enabled=1 AND fast_remaining>0 AND id!=?
ORDER BY id ASC LIMIT 1`, fromID)
	next, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		log.Printf("account: fast exhausted on id=%d, no alternate account available", fromID)
		return SwitchResult{
			FromID:  fromID,
			Message: "fast exhausted; no alternate account with remaining quota",
		}, nil
	}
	if err != nil {
		return SwitchResult{}, err
	}
	if err := s.setActiveUnlocked(next.ID); err != nil {
		return SwitchResult{}, err
	}
	msg := fmt.Sprintf("switched active account %d -> %d (%s); restart Chrome to apply session %s",
		fromID, next.ID, next.Name, next.SessionDir)
	log.Printf("account: %s", msg)
	return SwitchResult{
		Switched:     true,
		FromID:       fromID,
		ToID:         next.ID,
		ToName:       next.Name,
		ToSessionDir: next.SessionDir,
		Message:      msg,
	}, nil
}

func (s *Store) getActiveIDUnlocked() (int64, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key='active_account_id'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var id int64
	_, _ = fmt.Sscanf(v, "%d", &id)
	return id, nil
}

func (s *Store) setActiveUnlocked(id int64) error {
	a, err := s.getUnlocked(id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO meta(key, value) VALUES('active_account_id', ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprintf("%d", id))
	if err != nil {
		return err
	}
	return s.writeActiveSessionFile(a.SessionDir)
}

func (s *Store) writeActiveSessionFile(sessionDir string) error {
	tmp := s.activeSessionPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(sessionDir)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.activeSessionPath)
}

// ActiveSessionPath returns the path of the on-disk active session file.
func (s *Store) ActiveSessionPath() string {
	return s.activeSessionPath
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row rowScanner) (Account, error) {
	var (
		a         Account
		enabled   int
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&a.ID, &a.Name, &a.SessionDir, &a.FastRemaining, &enabled, &createdAt, &updatedAt); err != nil {
		return Account{}, err
	}
	a.Enabled = enabled != 0
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// IsSeedanceFast reports whether the API model name maps to Seedance Fast.
func IsSeedanceFast(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return true
	}
	if strings.Contains(m, "mini") {
		return false
	}
	return true
}

// QuotaCost returns local Seedance quota units consumed by a successful generation.
// Mini costs 1; Fast costs 2. Callers clamp to remaining so Fast can still run when
// only 1 unit is left (effective cost 1), allowing up to 3 Fast gens per day of 5.
func QuotaCost(model string) int {
	if IsSeedanceFast(model) {
		return QuotaCostFast
	}
	return QuotaCostMini
}
