//go:build windows

package wintundll

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

// Ensure makes the bundled Wintun driver available to this process, and
// returns an error explaining what a person should do if it cannot.
//
// Safe to call from anywhere, any number of times: the work happens once and
// every later call gets the same answer.
func Ensure() error {
	ensureOnce.Do(func() { ensureErr = install() })
	return ensureErr
}

var (
	ensureOnce sync.Once
	ensureErr  error
)

// dllName is what golang.zx2c4.com/wintun asks the loader for. It searches
// the application directory and System32 and nowhere else, which is why the
// file has to land beside the executable rather than somewhere tidier.
const dllName = "wintun.dll"

func install() error {
	if len(driver) == 0 {
		return fmt.Errorf("no Wintun driver is bundled for %s, so this build "+
			"cannot create a tunnel adapter", runtime.GOARCH)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find where this program is installed: %w", err)
	}
	// Symlinks resolved: writing beside the link rather than beside the real
	// executable would put the driver in a directory the loader never reads.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	target := filepath.Join(filepath.Dir(exe), dllName)

	if err := place(target, driver); err != nil {
		return err
	}
	return pin(target)
}

// pin loads the driver from the full path just verified.
//
// Without this, the first thing to load Wintun is the library's own
// LoadLibraryEx("wintun.dll", …), which resolves by name against the
// application directory — a fresh lookup of a file that could have been
// swapped in between. Loading it here, immediately, by absolute path, narrows
// that gap to nothing worth measuring, and every later lookup of the same
// file returns this already-loaded module rather than reading the disk again.
func pin(target string) error {
	const loadWithAlteredSearchPath = 0x00000008
	if _, err := windows.LoadLibraryEx(target, 0, loadWithAlteredSearchPath); err != nil {
		return fmt.Errorf("Windows refused to load %s: %w", target, err)
	}
	return nil
}
