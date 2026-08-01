package main

import (
	"bytes"
	"image/png"
	"testing"
)

// The four states have to be four different icons. Rendering them the same
// colour by accident is invisible in code review and obvious only on the
// taskbar of somebody who has already stopped trusting the app.
func TestEveryStateGetsItsOwnIcon(t *testing.T) {
	seen := map[string]string{}
	for _, phase := range []string{phaseIdle, phaseLinking, phaseActive, phaseDropped} {
		icon := trayIcon(phase)
		if len(icon) == 0 {
			t.Fatalf("%s produced no icon", phase)
		}
		key := string(icon)
		if other, clash := seen[key]; clash {
			t.Fatalf("%s and %s render identically", phase, other)
		}
		seen[key] = phase
	}
}

func TestIconsAreCached(t *testing.T) {
	first := trayIcon(phaseActive)
	second := trayIcon(phaseActive)
	if &first[0] != &second[0] {
		t.Fatal("the icon was re-rendered rather than cached")
	}
}

// On non-Windows the bytes are a plain PNG of the expected size.
func TestIconIsAValidImage(t *testing.T) {
	if isWindows {
		t.Skip("Windows wraps the PNG in an ICO container; checked separately")
	}
	img, err := png.Decode(bytes.NewReader(trayIcon(phaseActive)))
	if err != nil {
		t.Fatalf("the icon is not a decodable PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != iconSize || b.Dy() != iconSize {
		t.Fatalf("icon is %dx%d, want %dx%d", b.Dx(), b.Dy(), iconSize, iconSize)
	}
}

// The ring has to actually be drawn: an all-transparent icon is a blank gap
// in the tray, which looks like the app crashed.
func TestIconHasVisiblePixels(t *testing.T) {
	if isWindows {
		t.Skip("checked on the PNG path")
	}
	img, err := png.Decode(bytes.NewReader(trayIcon(phaseActive)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	opaque := 0
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
				opaque++
			}
		}
	}
	// A ring plus a core over a 32x32 field: well over fifty pixels, and
	// nowhere near all of them.
	if opaque < 50 {
		t.Fatalf("only %d pixels are drawn; the icon is effectively blank", opaque)
	}
	if opaque > iconSize*iconSize*3/4 {
		t.Fatalf("%d pixels are drawn; the icon is a solid block, not a ring", opaque)
	}
}

// Windows will not show an icon it cannot parse, and a wrong header is a
// silent failure — no icon, no error.
func TestICOHeaderIsWellFormed(t *testing.T) {
	pngBytes := []byte("fake-png-payload")
	ico := pngToICO(pngBytes)

	if len(ico) != 22+len(pngBytes) {
		t.Fatalf("ICO is %d bytes, want %d", len(ico), 22+len(pngBytes))
	}
	if ico[2] != 1 || ico[3] != 0 {
		t.Fatal("image type is not 1 (icon)")
	}
	if ico[4] != 1 || ico[5] != 0 {
		t.Fatal("image count is not 1")
	}
	if ico[6] != iconSize || ico[7] != iconSize {
		t.Fatalf("declared size is %dx%d, want %d", ico[6], ico[7], iconSize)
	}
	if !bytes.Equal(ico[22:], pngBytes) {
		t.Fatal("the payload is not at the declared offset")
	}
}
