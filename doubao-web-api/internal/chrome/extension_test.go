package chrome

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExtensionDirFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	ext := filepath.Join(root, "chrome-extension")
	if err := os.MkdirAll(ext, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"), []byte(`{"name":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(extensionEnv, "")
	got := ResolveExtensionDir(root)
	if got != ext {
		t.Fatalf("got %q want %q", got, ext)
	}
}

func TestResolveExtensionDirFromEnv(t *testing.T) {
	root := t.TempDir()
	ext := filepath.Join(root, "ext")
	if err := os.MkdirAll(ext, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(extensionEnv, ext)
	got := ResolveExtensionDir(filepath.Join(root, "unused"))
	if got != ext {
		t.Fatalf("got %q want %q", got, ext)
	}
}

func TestLoadExtensionArgs(t *testing.T) {
	if LoadExtensionArgs("") != nil {
		t.Fatal("empty dir should skip flags")
	}
	args := LoadExtensionArgs("/tmp/chrome-extension")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--load-extension=/tmp/chrome-extension") {
		t.Fatal(args)
	}
	if !strings.Contains(joined, "--enable-unsafe-extension-debugging") {
		t.Fatal(args)
	}
}

func TestMergeDisableFeatures(t *testing.T) {
	got := mergeDisableFeatures("CalculateNativeWinOcclusion", "DisableLoadExtensionCommandLineSwitch")
	if got != "CalculateNativeWinOcclusion,DisableLoadExtensionCommandLineSwitch" {
		t.Fatal(got)
	}
	if mergeDisableFeatures("A,A", "A") != "A" {
		t.Fatal(mergeDisableFeatures("A,A", "A"))
	}
}

func TestEnsureDeveloperMode(t *testing.T) {
	session := t.TempDir()
	if err := EnsureDeveloperMode(session); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(session, "Default", "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var pref map[string]any
	if json.Unmarshal(raw, &pref) != nil {
		t.Fatal(string(raw))
	}
	ext := pref["extensions"].(map[string]any)
	ui := ext["ui"].(map[string]any)
	if ui["developer_mode"] != true {
		t.Fatal(pref)
	}
}
