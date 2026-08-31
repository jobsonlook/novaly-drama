package chrome

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// BringToFront raises the Chrome OS window that listens on the given CDP port.
// Best-effort: failures are logged and ignored (headless / no GUI / permissions).
func BringToFront(cdpPort int) {
	if cdpPort <= 0 {
		return
	}
	pids, err := listenersOnPort(cdpPort)
	if err != nil || len(pids) == 0 {
		return
	}
	pid := pids[0]
	switch runtime.GOOS {
	case "darwin":
		if err := bringToFrontDarwin(pid); err != nil {
			log.Printf("chrome: bring to front port=%d pid=%d: %v", cdpPort, pid, err)
			return
		}
		log.Printf("chrome: brought window to front (port=%d pid=%d)", cdpPort, pid)
	case "linux":
		if err := bringToFrontLinux(pid); err != nil {
			log.Printf("chrome: bring to front port=%d pid=%d: %v", cdpPort, pid, err)
			return
		}
		log.Printf("chrome: brought window to front (port=%d pid=%d)", cdpPort, pid)
	default:
		// Windows / others: no-op
	}
}

func bringToFrontDarwin(pid int) error {
	script := fmt.Sprintf(`tell application "System Events"
  set frontmost of first process whose unix id is %d to true
end tell`, pid)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func bringToFrontLinux(pid int) error {
	// wmctrl / xdotool may be absent; try xdotool first.
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("xdotool not found")
	}
	cmd := exec.Command("xdotool", "search", "--pid", strconv.Itoa(pid), "windowactivate")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
