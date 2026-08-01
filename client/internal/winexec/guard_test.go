package winexec

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBareWindowsCommands fails if anything in the client launches a
// Windows helper program directly.
//
// The symptom of getting this wrong is not a test failure or a crash: it is a
// black console window flashing over whatever the person is doing, once per
// route added, every time they connect. Nobody running the tests on Linux or
// macOS will ever see it, and it is a one-word difference at the call site —
// exactly the kind of regression that only a guard catches.
func TestNoBareWindowsCommands(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// The programs that carry a console window with them. Adding one here is
	// how a new helper gets covered.
	consoleProgs := []string{"powershell", "cmd.exe", "netsh", "schtasks", "route.exe"}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_windows.go") {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(blob), "\n") {
			if !strings.Contains(line, "exec.Command(") || strings.Contains(line, "winexec.Command(") {
				continue
			}
			for _, prog := range consoleProgs {
				if strings.Contains(line, `"`+prog) {
					t.Errorf("%s:%d launches %s with exec.Command, which flashes a "+
						"console window; use winexec.Command", rel, i+1, prog)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
