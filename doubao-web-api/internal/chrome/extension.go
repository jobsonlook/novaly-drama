package chrome

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	extensionEnv     = "DOUBAO_CHROME_EXTENSION_DIR"
	chromeRootEnv    = "DOUBAO_CHROME_ROOT"
	extensionRelPath = "chrome-extension"
)

// ResolveExtensionDir finds the unpacked 「豆包无水印下载」extension directory.
func ResolveExtensionDir(repoRoot string) string {
	if p := absIfExt(os.Getenv(extensionEnv)); p != "" {
		return p
	}
	for _, root := range extensionSearchRoots(repoRoot) {
		if p := absIfExt(filepath.Join(root, extensionRelPath)); p != "" {
			return p
		}
	}
	return ""
}

func extensionSearchRoots(repoRoot string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}
	add(repoRoot)
	add(os.Getenv(chromeRootEnv))
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		add(dir)
		add(filepath.Dir(dir))
	}
	return out
}

func absIfExt(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	st, err := os.Stat(filepath.Join(abs, "manifest.json"))
	if err != nil || st.IsDir() {
		return ""
	}
	return abs
}

// LoadExtensionArgs are Chrome flags that load the unpacked extension on launch.
// Official Chrome 137+ may ignore --load-extension; CDP install is the fallback.
func LoadExtensionArgs(extDir string) []string {
	if extDir == "" {
		return nil
	}
	return []string{
		"--enable-unsafe-extension-debugging",
		"--load-extension=" + extDir,
	}
}

func mergeDisableFeatures(existing string, extra ...string) string {
	seen := map[string]bool{}
	var parts []string
	add := func(raw string) {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			parts = append(parts, p)
		}
	}
	add(existing)
	for _, e := range extra {
		add(e)
	}
	return strings.Join(parts, ",")
}

func disableFeaturesArg(existing string) string {
	merged := mergeDisableFeatures(existing, "DisableLoadExtensionCommandLineSwitch")
	if merged == "" {
		return ""
	}
	return "--disable-features=" + merged
}

// EnsureDeveloperMode turns on chrome://extensions developer mode for a new profile
// so the unpacked extension can stay visible after --load-extension.
func EnsureDeveloperMode(sessionDir string) error {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return nil
	}
	dir := filepath.Join(sessionDir, "Default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	prefPath := filepath.Join(dir, "Preferences")
	pref := map[string]any{}
	if raw, err := os.ReadFile(prefPath); err == nil && len(raw) > 0 {
		if json.Unmarshal(raw, &pref) != nil {
			return nil
		}
	}
	ext, _ := pref["extensions"].(map[string]any)
	if ext == nil {
		ext = map[string]any{}
		pref["extensions"] = ext
	}
	ui, _ := ext["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
		ext["ui"] = ui
	}
	if on, _ := ui["developer_mode"].(bool); on {
		return nil
	}
	ui["developer_mode"] = true
	raw, err := json.Marshal(pref)
	if err != nil {
		return err
	}
	return os.WriteFile(prefPath, raw, 0o600)
}
