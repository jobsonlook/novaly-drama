package chrome

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Browser is the CDP reconnect surface used after Chrome restarts.
type Browser interface {
	Close()
	Start(ctx context.Context) error
}

// Manager kills the CDP Chrome instance and starts scripts/start-chrome.sh again.
type Manager struct {
	CDPURL     string
	CDPPort    int
	ScriptPath string
	SessionDir string // if set, passed as DOUBAO_SESSION_DIR (skips active_session)
	Browser    Browser

	mu sync.Mutex
}

// NewManager builds a chrome session manager. scriptPath defaults to ./scripts/start-chrome.sh.
func NewManager(cdpURL string, cdpPort int, scriptPath string, browser Browser) *Manager {
	if cdpPort <= 0 {
		cdpPort = portFromURL(cdpURL)
	}
	if scriptPath == "" {
		scriptPath = "./scripts/start-chrome.sh"
	}
	return &Manager{
		CDPURL:     cdpURL,
		CDPPort:    cdpPort,
		ScriptPath: scriptPath,
		Browser:    browser,
	}
}

// NewManagerWithSession is like NewManager but binds Chrome to an explicit user-data-dir.
func NewManagerWithSession(cdpURL string, cdpPort int, scriptPath, sessionDir string, browser Browser) *Manager {
	m := NewManager(cdpURL, cdpPort, scriptPath, browser)
	m.SessionDir = sessionDir
	return m
}

// EnsureStarted connects to an existing CDP Chrome, or starts one when the CDP
// endpoint is unavailable. Unlike Restart, it never kills an existing process.
func (m *Manager) EnsureStarted(ctx context.Context) error {
	if m == nil || m.Browser == nil {
		return fmt.Errorf("chrome manager not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if cdpReady(ctx, m.CDPURL) {
		log.Printf("chrome: CDP already available at %s", m.CDPURL)
		if err := m.Browser.Start(ctx); err != nil {
			return fmt.Errorf("connect existing cdp: %w", err)
		}
		return nil
	}

	log.Printf("chrome: CDP unavailable at %s; starting via %s", m.CDPURL, m.ScriptPath)
	if err := m.startChrome(); err != nil {
		return err
	}
	if err := waitCDPReady(ctx, m.CDPURL, 45*time.Second); err != nil {
		return fmt.Errorf("wait cdp ready after start: %w", err)
	}
	if err := m.Browser.Start(ctx); err != nil {
		return fmt.Errorf("connect started cdp: %w", err)
	}
	log.Printf("chrome: startup complete")
	return nil
}

// Stop disconnects CDP and terminates Chrome listening on the debug port.
// Call this when the Go server is shutting down.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("chrome: stopping session (cdp=%s port=%d)", m.CDPURL, m.CDPPort)
	if m.Browser != nil {
		m.Browser.Close()
	}
	if err := killListenersOnPort(m.CDPPort); err != nil {
		return fmt.Errorf("kill chrome on port %d: %w", m.CDPPort, err)
	}
	waitCtx := ctx
	if waitCtx == nil {
		waitCtx = context.Background()
	}
	if err := waitPortClosed(waitCtx, m.CDPPort, 15*time.Second); err != nil {
		log.Printf("chrome: wait exit: %v", err)
		return err
	}
	log.Printf("chrome: stopped")
	return nil
}

// Restart closes CDP, kills Chrome on the debug port, starts the chrome script,
// waits for CDP, then reconnects the browser client.
func (m *Manager) Restart(ctx context.Context) error {
	if m == nil || m.Browser == nil {
		return fmt.Errorf("chrome manager not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("chrome: restarting session (cdp=%s port=%d script=%s)", m.CDPURL, m.CDPPort, m.ScriptPath)
	m.Browser.Close()

	if err := killListenersOnPort(m.CDPPort); err != nil {
		log.Printf("chrome: kill port %d: %v (continuing)", m.CDPPort, err)
	}
	if err := waitPortClosed(ctx, m.CDPPort, 15*time.Second); err != nil {
		return fmt.Errorf("wait chrome exit: %w", err)
	}

	if err := m.startChrome(); err != nil {
		return err
	}
	if err := waitCDPReady(ctx, m.CDPURL, 45*time.Second); err != nil {
		return fmt.Errorf("wait cdp ready: %w", err)
	}
	if err := m.Browser.Start(ctx); err != nil {
		return fmt.Errorf("reconnect cdp: %w", err)
	}
	log.Printf("chrome: restart complete")
	return nil
}

func (m *Manager) startChrome() error {
	script, err := filepath.Abs(m.ScriptPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("chrome script: %w", err)
	}
	cmd := exec.Command(script)
	cmd.Dir = filepath.Dir(filepath.Dir(script)) // repo root when script is scripts/start-chrome.sh
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("DOUBAO_CDP_PORT=%d", m.CDPPort),
	)
	if m.SessionDir != "" {
		cmd.Env = append(cmd.Env,
			"DOUBAO_SESSION_DIR="+m.SessionDir,
			"DOUBAO_IGNORE_ACTIVE_SESSION=1",
		)
	}
	logName := "chrome.log"
	if m.CDPPort > 0 {
		logName = fmt.Sprintf("chrome-%d.log", m.CDPPort)
	}
	logPath := filepath.Join(cmd.Dir, "data", logName)
	var logFile *os.File
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logFile = f
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	if logFile == nil {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		logPath = "(stdout)"
	}
	// Detach from Go process group so Chrome survives if server restarts later.
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = chromeSysProcAttr()
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return fmt.Errorf("start chrome script: %w", err)
	}
	log.Printf("chrome: started pid=%d via %s port=%d session=%q (logs → %s)",
		cmd.Process.Pid, script, m.CDPPort, m.SessionDir, logPath)
	// Do not block — Chrome keeps running.
	go func() {
		_ = cmd.Wait()
		if logFile != nil {
			_ = logFile.Close()
		}
	}()
	return nil
}

func portFromURL(cdpURL string) int {
	cdpURL = strings.TrimSpace(cdpURL)
	if cdpURL == "" {
		return 9222
	}
	// http://127.0.0.1:9222
	if i := strings.LastIndex(cdpURL, ":"); i >= 0 {
		portStr := cdpURL[i+1:]
		if j := strings.IndexAny(portStr, "/?"); j >= 0 {
			portStr = portStr[:j]
		}
		if n, err := strconv.Atoi(portStr); err == nil && n > 0 {
			return n
		}
	}
	return 9222
}

func killListenersOnPort(port int) error {
	pids, err := listenersOnPort(port)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		log.Printf("chrome: no process listening on :%d", port)
		return nil
	}
	for _, pid := range pids {
		log.Printf("chrome: sending SIGTERM to pid=%d (port %d)", pid, port)
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = proc.Signal(os.Interrupt)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		left, _ := listenersOnPort(port)
		if len(left) == 0 {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	pids, _ = listenersOnPort(port)
	for _, pid := range pids {
		log.Printf("chrome: SIGKILL pid=%d (port %d)", pid, port)
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = proc.Kill()
	}
	return nil
}

func listenersOnPort(port int) ([]int, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("auto restart chrome is not supported on windows")
	}
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		// lsof exits 1 when no match
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var pids []int
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids, nil
}

func waitPortClosed(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pids, err := listenersOnPort(port)
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			// Also ensure connect fails.
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
			if err != nil {
				return nil
			}
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("port %d still open", port)
}

func waitCDPReady(ctx context.Context, cdpURL string, timeout time.Duration) error {
	versionURL := strings.TrimRight(cdpURL, "/") + "/json/version"
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout waiting for %s: %v", versionURL, lastErr)
}

func cdpReady(ctx context.Context, cdpURL string) bool {
	versionURL := strings.TrimRight(cdpURL, "/") + "/json/version"
	checkCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, versionURL, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 1500 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
