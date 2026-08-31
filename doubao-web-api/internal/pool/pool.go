// Package pool runs multiple Chrome/CDP workers for concurrent Seedance video gens.
package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/account"
	"github.com/mask/ai/doubao-web-api/internal/cdp"
	"github.com/mask/ai/doubao-web-api/internal/chrome"
)

var (
	ErrClosed   = errors.New("worker pool closed")
	ErrNoWorker = errors.New("no chrome worker available")
)

// Config configures WorkerPool startup.
type Config struct {
	MaxParallel    int
	BaseCDPPort    int
	ChromeScript   string
	VideoUIMode    string
	AutoStart      bool
	FallbackCDPURL string // used when no accounts (single legacy Chrome)
}

// Worker is one account-bound Chrome + CDP browser.
type Worker struct {
	AccountID  int64
	Name       string
	SessionDir string
	CDPPort    int
	CDPURL     string
	Browser    *cdp.Browser
	Manager    *chrome.Manager

	busy bool
}

// Pool leases workers for concurrent video generation.
// Startup warms only one Chrome; additional workers are started on demand up to MaxParallel.
type Pool struct {
	accounts *account.Store
	cfg      Config

	mu      sync.Mutex
	workers []*Worker
	notify  chan struct{}
	closed  bool
}

// Start connects a single Chrome worker (lazy scale-up happens in Acquire).
// When the account store is empty, starts a single legacy worker on BaseCDPPort.
func Start(ctx context.Context, accounts *account.Store, cfg Config) (*Pool, error) {
	if cfg.MaxParallel < 1 {
		cfg.MaxParallel = 1
	}
	if cfg.BaseCDPPort <= 0 {
		cfg.BaseCDPPort = 9222
	}
	if cfg.ChromeScript == "" {
		cfg.ChromeScript = "./scripts/start-chrome.sh"
	}
	if cfg.VideoUIMode == "" {
		cfg.VideoUIMode = "skill"
	}
	if cfg.FallbackCDPURL == "" {
		cfg.FallbackCDPURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.BaseCDPPort)
	}

	p := &Pool{
		accounts: accounts,
		cfg:      cfg,
		notify:   make(chan struct{}, 1),
	}

	slot, err := p.planInitialSlot()
	if err != nil {
		return nil, err
	}

	w, err := p.startWorker(ctx, slot, 0)
	if err != nil {
		return nil, err
	}
	p.workers = append(p.workers, w)
	log.Printf("pool: worker[0] account=%s id=%d port=%d session=%q",
		w.Name, w.AccountID, w.CDPPort, w.SessionDir)

	loggedIn, err := w.Browser.IsLoggedIn(ctx)
	if err != nil {
		log.Printf("pool: worker[0] login check failed: %v", err)
	} else if !loggedIn {
		log.Printf("pool: worker[0] warning: not logged in — open doubao chat in that Chrome")
	}

	log.Printf("pool: ready with 1 worker (max_parallel=%d; extra Chromes start on concurrent demand)", cfg.MaxParallel)
	return p, nil
}

type slotPlan struct {
	AccountID  int64
	Name       string
	SessionDir string
}

func (p *Pool) planInitialSlot() (slotPlan, error) {
	slots, err := p.planSlots(1)
	if err != nil {
		return slotPlan{}, err
	}
	if len(slots) == 0 {
		return slotPlan{}, fmt.Errorf("pool: no workers to start")
	}
	return slots[0], nil
}

// planSlots picks up to n enabled accounts. The active account is the preferred
// default worker when it has quota; remaining slots are filled by account ID.
func (p *Pool) planSlots(n int) ([]slotPlan, error) {
	if n < 1 {
		n = 1
	}
	if p.accounts == nil {
		return []slotPlan{{Name: "default", SessionDir: ""}}, nil
	}
	enabled, err := p.accounts.ListEnabled()
	if err != nil {
		return nil, err
	}
	if len(enabled) == 0 {
		return []slotPlan{{Name: "default", SessionDir: ""}}, nil
	}
	activeID, _ := p.accounts.ActiveID()

	sort.SliceStable(enabled, func(i, j int) bool {
		if (enabled[i].FastRemaining > 0) != (enabled[j].FastRemaining > 0) {
			return enabled[i].FastRemaining > 0
		}
		if enabled[i].FastRemaining > 0 {
			iActive := enabled[i].ID == activeID
			jActive := enabled[j].ID == activeID
			if iActive != jActive {
				return iActive
			}
		}
		return enabled[i].ID < enabled[j].ID
	})

	if n > len(enabled) {
		n = len(enabled)
	}
	out := make([]slotPlan, 0, n)
	for i := 0; i < n; i++ {
		a := enabled[i]
		out = append(out, slotPlan{
			AccountID:  a.ID,
			Name:       a.Name,
			SessionDir: a.SessionDir,
		})
	}
	return out, nil
}

func (p *Pool) startWorker(ctx context.Context, slot slotPlan, index int) (*Worker, error) {
	port := p.cfg.BaseCDPPort + index
	cdpURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	browser := cdp.NewBrowser(cdpURL)
	browser.SetVideoUIMode(p.cfg.VideoUIMode)

	var mgr *chrome.Manager
	if p.cfg.AutoStart {
		if slot.SessionDir != "" {
			mgr = chrome.NewManagerWithSession(cdpURL, port, p.cfg.ChromeScript, slot.SessionDir, browser)
		} else {
			mgr = chrome.NewManager(cdpURL, port, p.cfg.ChromeScript, browser)
		}
		if err := mgr.EnsureStarted(ctx); err != nil {
			return nil, fmt.Errorf("pool: start worker %d (%s) on :%d: %w", index, slot.Name, port, err)
		}
	} else {
		if err := browser.Start(ctx); err != nil {
			return nil, fmt.Errorf("pool: connect worker %d on :%d: %w", index, port, err)
		}
	}

	return &Worker{
		AccountID:  slot.AccountID,
		Name:       slot.Name,
		SessionDir: slot.SessionDir,
		CDPPort:    port,
		CDPURL:     cdpURL,
		Browser:    browser,
		Manager:    mgr,
	}, nil
}

func (p *Pool) allowedIDs() map[int64]struct{} {
	ids := make(map[int64]struct{}, len(p.workers))
	for _, w := range p.workers {
		if w.AccountID != 0 {
			ids[w.AccountID] = struct{}{}
		}
	}
	return ids
}

// DefaultBrowser returns the first worker's browser (images/uploads).
func (p *Pool) DefaultBrowser() *cdp.Browser {
	if p == nil || len(p.workers) == 0 {
		return nil
	}
	return p.workers[0].Browser
}

// DefaultManager returns the first worker's chrome manager (admin default restart).
func (p *Pool) DefaultManager() *chrome.Manager {
	if p == nil || len(p.workers) == 0 {
		return nil
	}
	return p.workers[0].Manager
}

// OpenAccount opens (or restarts) a Chrome window for the given account so the
// admin can log in. Prefer an existing worker bound to that account; otherwise
// rebind an idle worker, or scale up a new Chrome when under MaxParallel.
func (p *Pool) OpenAccount(ctx context.Context, a account.Account) error {
	if p == nil {
		return ErrNoWorker
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	if !p.cfg.AutoStart {
		p.mu.Unlock()
		return fmt.Errorf("auto-start chrome disabled; run start-chrome.sh for %s manually", a.SessionDir)
	}

	for _, w := range p.workers {
		if w.AccountID == a.ID {
			if w.busy {
				p.mu.Unlock()
				return fmt.Errorf("账号 %s 正在生成视频，请稍后再选用", a.Name)
			}
			err := p.restartWorkerLocked(ctx, w, a)
			p.mu.Unlock()
			return err
		}
	}

	for _, w := range p.workers {
		if w.busy {
			continue
		}
		log.Printf("pool: rebind worker port=%d %s -> %s for admin select", w.CDPPort, w.Name, a.Name)
		err := p.restartWorkerLocked(ctx, w, a)
		p.mu.Unlock()
		return err
	}

	if len(p.workers) >= p.cfg.MaxParallel {
		p.mu.Unlock()
		return fmt.Errorf("所有 Chrome worker 都在忙，请等视频任务结束后再选用")
	}

	index := len(p.workers)
	p.mu.Unlock()

	slot := slotPlan{AccountID: a.ID, Name: a.Name, SessionDir: a.SessionDir}
	w, err := p.startWorker(ctx, slot, index)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		if w.Manager != nil {
			_ = w.Manager.Stop(ctx)
		}
		return ErrClosed
	}
	if len(p.workers) >= p.cfg.MaxParallel {
		if w.Manager != nil {
			_ = w.Manager.Stop(ctx)
		}
		return fmt.Errorf("已达到 max_parallel=%d", p.cfg.MaxParallel)
	}
	// Port index may have raced; prefer append order matching startWorker index.
	if len(p.workers) != index {
		// Another scale-up won the slot; shut ours down.
		if w.Manager != nil {
			_ = w.Manager.Stop(ctx)
		}
		return fmt.Errorf("worker 池已变化，请重试选用")
	}
	p.workers = append(p.workers, w)
	log.Printf("pool: scaled for admin select worker[%d] account=%s port=%d", index, w.Name, w.CDPPort)
	p.signal()
	return nil
}

func (p *Pool) restartWorkerLocked(ctx context.Context, w *Worker, a account.Account) error {
	w.AccountID = a.ID
	w.Name = a.Name
	w.SessionDir = a.SessionDir

	if w.Manager == nil {
		w.Manager = chrome.NewManagerWithSession(w.CDPURL, w.CDPPort, p.cfg.ChromeScript, a.SessionDir, w.Browser)
	} else {
		w.Manager.SessionDir = a.SessionDir
	}

	log.Printf("pool: opening chrome for account=%s id=%d port=%d session=%q",
		a.Name, a.ID, w.CDPPort, a.SessionDir)
	if err := w.Manager.Restart(ctx); err != nil {
		return err
	}
	p.signal()
	return nil
}

// Workers returns a snapshot of worker status for logging/admin.
func (p *Pool) Workers() []WorkerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]WorkerStatus, 0, len(p.workers))
	for _, w := range p.workers {
		remaining := -1
		if p.accounts != nil && w.AccountID != 0 {
			if a, err := p.accounts.Get(w.AccountID); err == nil {
				remaining = a.FastRemaining
			}
		}
		out = append(out, WorkerStatus{
			AccountID:  w.AccountID,
			Name:       w.Name,
			SessionDir: w.SessionDir,
			CDPPort:    w.CDPPort,
			Busy:       w.busy,
			Remaining:  remaining,
		})
	}
	return out
}

// WorkerStatus is a read-only snapshot.
type WorkerStatus struct {
	AccountID  int64
	Name       string
	SessionDir string
	CDPPort    int
	Busy       bool
	Remaining  int
}

// Acquire blocks until a free worker with remaining quota is leased, or ctx ends.
// Scales up an extra Chrome (up to MaxParallel) when all current workers are busy
// and another account still has quota. Rebinds an idle worker when a warm account
// is exhausted (including while a peer worker is still leased) and an outside
// account still has remaining.
func (p *Pool) Acquire(ctx context.Context) (*Worker, error) {
	if p == nil {
		return nil, ErrNoWorker
	}
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, ErrClosed
		}

		w, err := p.tryAcquireLocked()
		if err == nil {
			p.mu.Unlock()
			log.Printf("pool: acquired worker account=%s id=%d port=%d", w.Name, w.AccountID, w.CDPPort)
			return w, nil
		}

		// All busy (or warm accounts leased): try starting another Chrome.
		if errors.Is(err, account.ErrAllLeased) || errors.Is(err, account.ErrNoQuotaAvailable) {
			if p.canScaleUpLocked() {
				cand, lookErr := p.findScaleUpAccountLocked()
				if lookErr != nil {
					p.mu.Unlock()
					return nil, lookErr
				}
				if cand != nil {
					index := len(p.workers)
					p.mu.Unlock()

					log.Printf("pool: scaling up worker[%d] for concurrent demand -> account=%s id=%d session=%q",
						index, cand.Name, cand.ID, cand.SessionDir)
					w, startErr := p.scaleUp(ctx, *cand, index)
					if startErr != nil {
						log.Printf("pool: scale-up failed: %v", startErr)
						// Fall through to wait/retry rather than failing the task immediately.
					} else {
						p.mu.Lock()
						lease, leaseErr := p.accounts.TryLease(cand.ID)
						if leaseErr != nil {
							w.busy = false
							p.mu.Unlock()
							p.signal()
							continue
						}
						w.AccountID = lease.Account.ID
						w.Name = lease.Account.Name
						w.SessionDir = lease.Account.SessionDir
						w.busy = true
						p.mu.Unlock()
						log.Printf("pool: acquired scaled worker account=%s id=%d port=%d", w.Name, w.AccountID, w.CDPPort)
						return w, nil
					}
					// scale-up failed — continue loop
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(500 * time.Millisecond):
					}
					continue
				}
			}
		}

		// Warm pool has no free quota account (exhausted and/or peers still leased).
		// Rebind an idle Chrome onto an outside account that still has remaining —
		// including the concurrent case: one worker exhausted+idle while another
		// account with quota is busy (ErrAllLeased), and a third account is free.
		if errors.Is(err, account.ErrNoQuotaAvailable) || errors.Is(err, account.ErrAllLeased) {
			cand, idle, lookErr := p.findRebindCandidateLocked()
			if lookErr != nil {
				p.mu.Unlock()
				return nil, lookErr
			}
			if cand == nil {
				if errors.Is(err, account.ErrNoQuotaAvailable) {
					msg := p.explainNoQuotaLocked()
					p.mu.Unlock()
					if msg != "" {
						return nil, fmt.Errorf("%w (%s)", account.ErrNoQuotaAvailable, msg)
					}
					return nil, account.ErrNoQuotaAvailable
				}
				// ErrAllLeased with nowhere to rebind — wait for a peer Release.
			} else if idle == nil {
				p.mu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-p.notify:
				case <-time.After(500 * time.Millisecond):
				}
				continue
			} else {
				idle.busy = true
				port := idle.CDPPort
				p.mu.Unlock()

				log.Printf("pool: rebinding idle worker port=%d -> account=%s id=%d session=%q",
					port, cand.Name, cand.ID, cand.SessionDir)
				if err := p.rebindAndRestart(ctx, idle, *cand); err != nil {
					p.mu.Lock()
					idle.busy = false
					p.mu.Unlock()
					p.signal()
					return nil, fmt.Errorf("rebind chrome to %s: %w", cand.Name, err)
				}

				p.mu.Lock()
				if p.closed {
					idle.busy = false
					p.mu.Unlock()
					return nil, ErrClosed
				}
				lease, leaseErr := p.accounts.TryLease(cand.ID)
				if leaseErr != nil {
					idle.busy = false
					p.mu.Unlock()
					p.signal()
					if errors.Is(leaseErr, account.ErrNoQuotaAvailable) || errors.Is(leaseErr, account.ErrAlreadyLeased) {
						continue
					}
					return nil, leaseErr
				}
				idle.AccountID = lease.Account.ID
				idle.Name = lease.Account.Name
				idle.SessionDir = lease.Account.SessionDir
				p.mu.Unlock()
				log.Printf("pool: acquired rebound worker account=%s id=%d port=%d", idle.Name, idle.AccountID, idle.CDPPort)
				return idle, nil
			}
		}

		// ErrAllLeased / busy — wait for Release or scale-up by another goroutine.
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.notify:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (p *Pool) canScaleUpLocked() bool {
	return p.cfg.MaxParallel > len(p.workers) && p.accounts != nil
}

func (p *Pool) findScaleUpAccountLocked() (*account.Account, error) {
	return p.findOutsideQuotaAccountLocked()
}

func (p *Pool) findOutsideQuotaAccountLocked() (*account.Account, error) {
	if p.accounts == nil {
		return nil, ErrNoWorker
	}
	enabled, err := p.accounts.ListEnabled()
	if err != nil {
		return nil, err
	}
	inPool := p.allowedIDs()
	for i := range enabled {
		a := &enabled[i]
		if a.FastRemaining <= 0 {
			continue
		}
		if _, ok := inPool[a.ID]; ok {
			continue
		}
		return a, nil
	}
	return nil, nil
}

func (p *Pool) scaleUp(ctx context.Context, a account.Account, index int) (*Worker, error) {
	if !p.cfg.AutoStart {
		// Unit tests: create in-memory worker without Chrome.
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed || len(p.workers) >= p.cfg.MaxParallel || len(p.workers) != index {
			return nil, fmt.Errorf("cannot scale up")
		}
		w := &Worker{
			AccountID:  a.ID,
			Name:       a.Name,
			SessionDir: a.SessionDir,
			CDPPort:    p.cfg.BaseCDPPort + index,
			CDPURL:     fmt.Sprintf("http://127.0.0.1:%d", p.cfg.BaseCDPPort+index),
		}
		p.workers = append(p.workers, w)
		p.signal()
		return w, nil
	}

	slot := slotPlan{AccountID: a.ID, Name: a.Name, SessionDir: a.SessionDir}
	w, err := p.startWorker(ctx, slot, index)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || len(p.workers) >= p.cfg.MaxParallel || len(p.workers) != index {
		if w.Manager != nil {
			_ = w.Manager.Stop(ctx)
		} else if w.Browser != nil {
			w.Browser.Close()
		}
		return nil, fmt.Errorf("scale-up race: pool changed")
	}
	p.workers = append(p.workers, w)
	log.Printf("pool: worker[%d] account=%s id=%d port=%d session=%q (on-demand)",
		index, w.Name, w.AccountID, w.CDPPort, w.SessionDir)
	p.signal()
	return w, nil
}

// findRebindCandidateLocked returns an enabled account with remaining quota that is
// not already bound to a pool worker, plus an idle worker to host it.
func (p *Pool) findRebindCandidateLocked() (*account.Account, *Worker, error) {
	cand, err := p.findOutsideQuotaAccountLocked()
	if err != nil || cand == nil {
		return nil, nil, err
	}
	var idle *Worker
	for _, w := range p.workers {
		if !w.busy {
			idle = w
			break
		}
	}
	return cand, idle, nil
}

func (p *Pool) explainNoQuotaLocked() string {
	if p.accounts == nil {
		return ""
	}
	var warm []string
	for _, w := range p.workers {
		rem := "?"
		if a, err := p.accounts.Get(w.AccountID); err == nil {
			rem = fmt.Sprintf("%d", a.FastRemaining)
		}
		warm = append(warm, fmt.Sprintf("%s(remaining=%s,busy=%v)", w.Name, rem, w.busy))
	}
	enabled, err := p.accounts.ListEnabled()
	if err != nil {
		return strings.Join(warm, ", ")
	}
	var outside []string
	inPool := p.allowedIDs()
	for _, a := range enabled {
		if a.FastRemaining <= 0 {
			continue
		}
		if _, ok := inPool[a.ID]; ok {
			continue
		}
		outside = append(outside, fmt.Sprintf("%s(%d)", a.Name, a.FastRemaining))
	}
	if len(outside) == 0 {
		return "warm workers: " + strings.Join(warm, ", ")
	}
	return fmt.Sprintf("workers: %s; other accounts with quota: %s — will scale/rebind when possible",
		strings.Join(warm, ", "), strings.Join(outside, ", "))
}

func (p *Pool) rebindAndRestart(ctx context.Context, w *Worker, a account.Account) error {
	if w == nil {
		return ErrNoWorker
	}
	w.AccountID = a.ID
	w.Name = a.Name
	w.SessionDir = a.SessionDir
	if w.Manager == nil {
		if !p.cfg.AutoStart {
			return nil
		}
		w.Manager = chrome.NewManagerWithSession(w.CDPURL, w.CDPPort, p.cfg.ChromeScript, a.SessionDir, w.Browser)
	} else {
		w.Manager.SessionDir = a.SessionDir
	}
	return w.Manager.Restart(ctx)
}

func (p *Pool) tryAcquireLocked() (*Worker, error) {
	// Legacy single worker with no account binding.
	if len(p.workers) == 1 && p.workers[0].AccountID == 0 {
		w := p.workers[0]
		if w.busy {
			return nil, account.ErrAllLeased
		}
		w.busy = true
		return w, nil
	}

	allowed := p.allowedIDs()
	if p.accounts == nil {
		return nil, ErrNoWorker
	}

	lease, err := p.accounts.AcquireForVideoFrom(allowed)
	if err != nil {
		return nil, err
	}

	for _, w := range p.workers {
		if w.AccountID == lease.Account.ID {
			if w.busy {
				p.accounts.Release(lease.Account.ID)
				return nil, account.ErrAllLeased
			}
			w.busy = true
			w.Name = lease.Account.Name
			return w, nil
		}
	}
	p.accounts.Release(lease.Account.ID)
	return nil, ErrNoWorker
}

// Release marks the worker idle and drops its account lease.
func (p *Pool) Release(w *Worker) {
	if p == nil || w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	w.busy = false
	if p.accounts != nil && w.AccountID != 0 {
		p.accounts.Release(w.AccountID)
	}
	log.Printf("pool: released worker account=%s id=%d port=%d", w.Name, w.AccountID, w.CDPPort)
	p.signal()
}

func (p *Pool) signal() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// Stop disconnects all browsers and optionally kills Chromes.
func (p *Pool) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.signal()
	return p.stopWorkersCtx(ctx)
}

func (p *Pool) stopWorkers() {
	_ = p.stopWorkersCtx(context.Background())
}

func (p *Pool) stopWorkersCtx(ctx context.Context) error {
	p.mu.Lock()
	workers := append([]*Worker(nil), p.workers...)
	p.mu.Unlock()

	var firstErr error
	for i := len(workers) - 1; i >= 0; i-- {
		w := workers[i]
		if w.Manager != nil {
			if err := w.Manager.Stop(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if w.Browser != nil {
			w.Browser.Close()
		}
	}
	return firstErr
}

// Len returns the number of workers.
func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.workers)
}
