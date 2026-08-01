package wintundll

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestPlaceWritesWhenMissing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "wintun.dll")
	want := []byte("driver bytes")

	if err := place(target, want); err != nil {
		t.Fatalf("place: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}

// TestPlaceReplacesDifferentBytes is the one that matters. A wintun.dll that
// is not ours is either a stale version or something planted there, and this
// process loads it with administrator rights either way.
func TestPlaceReplacesDifferentBytes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "wintun.dll")
	if err := os.WriteFile(target, []byte("something else entirely"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := []byte("driver bytes")
	if err := place(target, want); err != nil {
		t.Fatalf("place: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("left %q in place, want %q", got, want)
	}
}

// TestPlaceLeavesMatchingFileAlone: rewriting the driver on every connect
// would fail as soon as it is loaded, and there is nothing to gain from it.
func TestPlaceLeavesMatchingFileAlone(t *testing.T) {
	target := filepath.Join(t.TempDir(), "wintun.dll")
	want := []byte("driver bytes")
	if err := os.WriteFile(target, want, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	if err := place(target, want); err != nil {
		t.Fatalf("place: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("rewrote a file that already held the right bytes")
	}
}

// TestPlaceLeavesNoTempFiles: the driver lands next to the executable, and
// littering a program's own directory with .tmp files on every connect is the
// kind of thing people notice and distrust.
func TestPlaceLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wintun.dll")

	for range 3 {
		if err := place(target, []byte("driver bytes")); err != nil {
			t.Fatalf("place: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "wintun.dll" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory holds %v, want only wintun.dll", names)
	}
}

func TestPlaceReportsUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := place(filepath.Join(dir, "wintun.dll"), []byte("x")); err == nil {
		t.Fatal("writing into a read-only directory reported success")
	}
}

// TestBundledDriverIsTheOfficialBuild pins the embedded bytes to the hashes
// of the amd64 and arm64 DLLs inside wintun-0.14.1.zip, whose own SHA2-256 is
// published at wintun.net/builds.
//
// The driver is signed by WireGuard LLC, and an unsigned copy cannot load —
// so a change here is either a deliberate version bump, in which case these
// hashes move with it, or something that must never ship.
func TestBundledDriverIsTheOfficialBuild(t *testing.T) {
	for _, tc := range []struct{ arch, sum string }{
		{"amd64", "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"},
		{"arm64", "f7ba89005544be9d85231a9e0d5f23b2d15b3311667e2dad0debd344918a3f80"},
	} {
		blob, err := os.ReadFile(filepath.Join("bin", "wintun-"+tc.arch+".dll"))
		if err != nil {
			t.Fatalf("%s: %v", tc.arch, err)
		}
		if got := sha256.Sum256(blob); hex(got[:]) != tc.sum {
			t.Fatalf("bin/wintun-%s.dll is %s, want %s", tc.arch, hex(got[:]), tc.sum)
		}
	}
}

func hex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0xf])
	}
	return string(out)
}
