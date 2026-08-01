package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// The four states the tray icon shows, matching the app's own vocabulary.
const (
	phaseIdle    = "idle"
	phaseLinking = "linking"
	phaseActive  = "active"
	phaseDropped = "dropped"
)

// trayIcon returns the notification-area icon for a state.
//
// Drawn rather than shipped as four image files. A tray icon is a 16-pixel
// ring in one colour; generating it keeps the four states guaranteed
// identical in shape and different only in hue, which is the whole point,
// and means changing the palette does not mean re-exporting artwork.
//
// Results are cached because this is called on every status poll.
func trayIcon(phase string) []byte {
	iconCache.mu.Lock()
	defer iconCache.mu.Unlock()

	if b, ok := iconCache.icons[phase]; ok {
		return b
	}
	b := drawIcon(phaseColor(phase))
	iconCache.icons[phase] = b
	return b
}

var iconCache = struct {
	mu    sync.Mutex
	icons map[string][]byte
}{icons: map[string][]byte{}}

func phaseColor(phase string) color.NRGBA {
	switch phase {
	case phaseActive:
		return color.NRGBA{R: 0x22, G: 0xE5, B: 0xC8, A: 0xFF} // live
	case phaseLinking:
		return color.NRGBA{R: 0xFF, G: 0xA7, B: 0x33, A: 0xFF} // work
	case phaseDropped:
		return color.NRGBA{R: 0xFF, G: 0x3D, B: 0x5E, A: 0xFF} // fail
	default:
		return color.NRGBA{R: 0x8F, G: 0xA0, B: 0xBD, A: 0xFF} // ink-dim
	}
}

// iconSize is 32px so the icon stays sharp on the 150% and 200% scaling
// most Windows laptops ship with; the tray downsamples it for 100%.
const iconSize = 32

// drawIcon renders a ring with a filled centre — the reactor core at 32
// pixels. Anything more detailed disappears at this size.
func drawIcon(c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))

	const (
		centre    = iconSize / 2
		ringOuter = 14.0
		ringInner = 11.0
		coreR     = 6.0
	)

	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx := float64(x) - centre + 0.5
			dy := float64(y) - centre + 0.5
			d := math.Hypot(dx, dy)

			// Coverage rather than a hard cutoff: a 32px circle with
			// aliased edges reads as a smudge in the tray.
			alpha := coverage(d, ringInner, ringOuter) // the ring
			if a := coverage(d, 0, coreR); a > alpha {
				alpha = a
			}
			if alpha <= 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: c.R, G: c.G, B: c.B,
				A: uint8(math.Round(float64(c.A) * alpha)),
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	if isWindows {
		return pngToICO(buf.Bytes())
	}
	return buf.Bytes()
}

// coverage returns how much of a pixel at distance d falls inside the
// annulus between inner and outer, antialiased over one pixel at each edge.
func coverage(d, inner, outer float64) float64 {
	a := clamp01(outer-d+0.5) * clamp01(d-inner+0.5)
	if inner == 0 {
		a = clamp01(outer - d + 0.5)
	}
	return a
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// pngToICO wraps a PNG in an ICO container.
//
// Windows' tray wants an .ico, but every supported Windows accepts a PNG
// inside one — the format has allowed it since Vista — so this is a 22-byte
// header rather than a bitmap encoder.
func pngToICO(pngData []byte) []byte {
	var buf bytes.Buffer

	// ICONDIR: reserved, type 1 (icon), one image.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))

	// ICONDIRENTRY.
	buf.WriteByte(iconSize)                             // width
	buf.WriteByte(iconSize)                             // height
	buf.WriteByte(0)                                    // palette size, 0 for truecolour
	buf.WriteByte(0)                                    // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // colour planes
	binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
	binary.Write(&buf, binary.LittleEndian, uint32(len(pngData)))
	binary.Write(&buf, binary.LittleEndian, uint32(22)) // offset past this header

	buf.Write(pngData)
	return buf.Bytes()
}
