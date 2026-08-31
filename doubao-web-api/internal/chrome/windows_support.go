package chrome

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func windowsChromeCommand(session string, port int) (*exec.Cmd, error) {
	var candidates []string
	if p := os.Getenv("CHROME_BIN"); p != "" {
		candidates = append(candidates, p)
	}
	for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
		if p := os.Getenv(key); p != "" {
			candidates = append(candidates, filepath.Join(p, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	var binary string
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			binary = p
			break
		}
	}
	if binary == "" {
		return nil, fmt.Errorf("未找到 Google Chrome，请安装 Chrome 或设置 CHROME_BIN 为 chrome.exe 的完整路径")
	}
	if session == "" {
		session = os.Getenv("DOUBAO_SESSION_DIR")
	}
	if session == "" {
		session = "session"
	}
	session, err := filepath.Abs(session)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(session, 0700); err != nil {
		return nil, err
	}
	return exec.Command(binary, fmt.Sprintf("--remote-debugging-port=%d", port), "--remote-debugging-address=127.0.0.1", "--user-data-dir="+session, "--no-first-run", "--no-default-browser-check", "--disable-background-timer-throttling", "--disable-renderer-backgrounding", "https://www.doubao.com/chat/"), nil
}
func parseWindowsListeners(output string, port int) []int {
	var out []int
	seen := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "TCP" || f[3] != "LISTENING" {
			continue
		}
		_, p, err := net.SplitHostPort(f[1])
		if err != nil || p != strconv.Itoa(port) {
			continue
		}
		pid, err := strconv.Atoi(f[4])
		if err == nil && pid > 0 && !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	}
	return out
}
func stopWindowsChrome(port int) error {
	pids, err := listenersOnPort(port)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		// Never terminate an unrelated application that happens to occupy the port.
		out, err := exec.Command("tasklist.exe", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			return err
		}
		records, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
		if err != nil {
			return err
		}
		found := false
		for _, row := range records {
			if len(row) >= 2 && strings.EqualFold(row[0], "chrome.exe") && row[1] == strconv.Itoa(pid) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("端口 %d 的进程 %d 不是 Chrome，拒绝停止", port, pid)
		}
		// Ask Chrome to close normally; do not force-kill profiles or unrelated windows.
		if out, err := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T").CombinedOutput(); err != nil {
			return fmt.Errorf("请手动关闭专用 Chrome 窗口: %s (%w)", strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}
