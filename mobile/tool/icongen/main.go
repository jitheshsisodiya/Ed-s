// Generates NexusVPN's Android launcher icons: the same reactor core the
// desktop draws in its tray, rendered at each density Android asks for.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	live   = color.NRGBA{0x22, 0xE5, 0xC8, 0xFF}
	ground = color.NRGBA{0x05, 0x08, 0x0F, 0xFF}
)

func main() {
	res := os.Args[1]

	// Legacy square icons: the mark on the deck's own ground.
	for dir, size := range map[string]int{
		"mipmap-mdpi": 48, "mipmap-hdpi": 72, "mipmap-xhdpi": 96,
		"mipmap-xxhdpi": 144, "mipmap-xxxhdpi": 192,
	} {
		write(filepath.Join(res, dir, "ic_launcher.png"), draw(size, 1.0, true))
	}

	// Adaptive foreground: Android crops adaptive icons to whatever shape the
	// launcher uses, and only the middle two thirds is guaranteed to survive,
	// so the mark is drawn smaller on a transparent field.
	for dir, size := range map[string]int{
		"mipmap-mdpi": 108, "mipmap-hdpi": 162, "mipmap-xhdpi": 216,
		"mipmap-xxhdpi": 324, "mipmap-xxxhdpi": 432,
	} {
		write(filepath.Join(res, dir, "ic_launcher_foreground.png"), draw(size, 0.58, false))
	}
}

// draw renders the ring and core at size pixels. scale shrinks the mark
// within the canvas; opaque fills the background rather than leaving it clear.
func draw(size int, scale float64, opaque bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	if opaque {
		for i := range img.Pix {
			switch i % 4 {
			case 0:
				img.Pix[i] = ground.R
			case 1:
				img.Pix[i] = ground.G
			case 2:
				img.Pix[i] = ground.B
			case 3:
				img.Pix[i] = 0xFF
			}
		}
	}

	centre := float64(size) / 2
	unit := float64(size) * scale
	ringOuter, ringInner, coreR := unit*0.44, unit*0.345, unit*0.19

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-centre+0.5, float64(y)-centre+0.5)
			a := annulus(d, ringInner, ringOuter)
			if c := clamp01(coreR - d + 0.5); c > a {
				a = c
			}
			if a <= 0 {
				continue
			}
			img.Set(x, y, over(live, a, img.NRGBAAt(x, y)))
		}
	}
	return img
}

func annulus(d, inner, outer float64) float64 {
	return clamp01(outer-d+0.5) * clamp01(d-inner+0.5)
}

// over composites src at alpha a onto dst, so the mark's antialiased edge
// blends into the ground instead of showing a fringe of transparency.
func over(src color.NRGBA, a float64, dst color.NRGBA) color.NRGBA {
	da := float64(dst.A) / 255
	outA := a + da*(1-a)
	if outA == 0 {
		return color.NRGBA{}
	}
	mix := func(s, d uint8) uint8 {
		return uint8(math.Round((float64(s)*a + float64(d)*da*(1-a)) / outA))
	}
	return color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(math.Round(outA * 255))}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func write(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
