package wintundll

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// place writes want to target unless those exact bytes are already there.
//
// The comparison is a hash of the whole file, not a timestamp or a size,
// because this is the one file in the installation that Windows loads into an
// already-elevated process to reach the kernel's networking path. If
// something else has left a different wintun.dll there, the honest options
// are to replace it or to refuse — never to load it and hope.
//
// Platform-independent so it can be tested somewhere other than Windows;
// only Windows ever calls it.
func place(target string, want []byte) error {
	sum := sha256.Sum256(want)

	switch got, err := hashFile(target); {
	case err == nil && bytes.Equal(got, sum[:]):
		return nil
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("could not read %s: %w", target, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".wintun-*.tmp")
	if err != nil {
		return fmt.Errorf("could not write to %s — try running NexusVPN as "+
			"administrator, or move it out of a read-only folder: %w",
			filepath.Dir(target), err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename below has succeeded

	if _, err := tmp.Write(want); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", name, err)
	}
	// Flushed before the rename, so a machine that loses power mid-install
	// does not come back with a truncated driver in place.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", name, err)
	}

	if err := os.Rename(name, target); err != nil {
		// The usual cause is another copy of NexusVPN holding the old driver
		// open. Loading whatever is already there instead would mean running
		// a driver these bytes were never checked against.
		return fmt.Errorf("could not replace %s — close any other copy of "+
			"NexusVPN and try again: %w", target, err)
	}
	return nil
}

func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
