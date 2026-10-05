package chrome

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseWindowsListeners(t *testing.T) {
	output := `  TCP    127.0.0.1:9322    0.0.0.0:0     LISTENING       4321
  TCP    [::]:9322        [::]:0         LISTENING       4321
  TCP    127.0.0.1:93220   0.0.0.0:0     LISTENING       9999
  TCP    127.0.0.1:9322    127.0.0.1:123  ESTABLISHED     7777
  UDP    127.0.0.1:9322    *:*                             5555
  TCP    127.0.0.1:9322    0.0.0.0:0     LISTENING       invalid`
	if got := parseWindowsListeners(output, 9322); !reflect.DeepEqual(got, []int{4321}) {
		t.Fatal(got)
	}
}
func TestWindowsChromeCommandKeepsSpacedPaths(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "Google Chrome.exe")
	if err := os.WriteFile(binary, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHROME_BIN", binary)
	session := filepath.Join(root, "Browser Profile")
	cmd, err := windowsChromeCommand(session, 9322)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != binary {
		t.Fatal(cmd.Path)
	}
	found := false
	for _, arg := range cmd.Args {
		if arg == "--user-data-dir="+session {
			found = true
		}
	}
	if !found {
		t.Fatal(cmd.Args)
	}
	ext := filepath.Join(root, "chrome-extension")
	if err := os.MkdirAll(ext, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(extensionEnv, ext)
	cmd, err = windowsChromeCommand(session, 9322)
	if err != nil {
		t.Fatal(err)
	}
	wantLoad := "--load-extension=" + ext
	foundLoad := false
	for _, arg := range cmd.Args {
		if arg == wantLoad {
			foundLoad = true
		}
	}
	if !foundLoad {
		t.Fatal(cmd.Args)
	}
	if st, err := os.Stat(session); err != nil || !st.IsDir() {
		t.Fatal("profile directory was not created")
	}
}
