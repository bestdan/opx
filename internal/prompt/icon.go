// Package prompt — icon embedding (consumed by the macOS dialog).
//
// We ship a single white-on-transparent PNG and recolor it at prompt time
// based on the current macOS appearance (light → near-black, dark →
// near-white). The recolored PNG is written to a per-user cache file and
// referenced from AppleScript via `with icon file POSIX file`.
//
// One source asset, pure stdlib, no extra build tooling. AppleScript's
// `display dialog` accepts PNG since macOS 10.10, so we don't need .icns.
package prompt

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed assets/opx.png
var iconPNG []byte

// foreground colors mirror macOS system label colors.
var (
	fgLight = color.NRGBA{R: 29, G: 29, B: 31, A: 255}    // dark ink for light mode
	fgDark  = color.NRGBA{R: 235, G: 235, B: 237, A: 255} // light ink for dark mode
)

// writeIconFile decodes the embedded white-on-transparent icon, recolors it
// for the current appearance, writes the result to a per-user cache path,
// and returns that path. Returns "" on any failure; callers must fall back
// to a built-in AppleScript icon.
//
// We rewrite on every prompt so flipping appearance with the binary already
// running is picked up on the next invocation.
func writeIconFile() string {
	src, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return ""
	}
	fg := fgLight
	name := "opx-light.png"
	if isDarkMode() {
		fg = fgDark
		name = "opx-dark.png"
	}
	tinted := recolor(src, fg)

	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "opx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return ""
	}
	if err := png.Encode(f, tinted); err != nil {
		f.Close()
		os.Remove(path)
		return ""
	}
	if err := f.Close(); err != nil {
		// Close can surface late write errors (full disk, etc.). Drop the
		// half-written file so AppleScript falls back instead of trying to
		// load a truncated PNG.
		os.Remove(path)
		return ""
	}
	return path
}

// recolor replaces every pixel's RGB with fg.RGB while preserving alpha.
// The source uses straight-alpha white pixels (R=G=B=255 at varying A for
// antialiasing), so this is a pure color swap — edge softness is preserved
// because we keep the alpha channel untouched.
func recolor(src image.Image, fg color.NRGBA) image.Image {
	b := src.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA() // a is 0..0xffff
			out.SetNRGBA(x, y, color.NRGBA{R: fg.R, G: fg.G, B: fg.B, A: uint8(a >> 8)})
		}
	}
	return out
}

// isDarkMode reports whether macOS is currently in Dark Appearance.
//
// `defaults read -g AppleInterfaceStyle` returns "Dark" in dark mode and
// exits non-zero in light mode (the key is absent). Any error → light, which
// matches the historical default and is safe on non-darwin builds (where
// `defaults` either fails or doesn't exist).
func isDarkMode() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "dark")
}
