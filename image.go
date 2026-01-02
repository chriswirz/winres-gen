package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"

	// Registered for their side effect: these are the source formats we accept.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
)

// fitMode says what to do when the source image is not already square.
type fitMode string

const (
	// modePad centres the image on a square canvas, adding background on the
	// two short sides. Nothing is lost, but the artwork ends up smaller.
	modePad fitMode = "pad"
	// modeCrop takes the largest centred square, filling the frame at the cost
	// of trimming the long edge.
	modeCrop fitMode = "crop"
)

func parseMode(s string) (fitMode, error) {
	switch fitMode(s) {
	case modePad, modeCrop:
		return fitMode(s), nil
	default:
		return "", fmt.Errorf("unknown mode %q; use \"pad\" or \"crop\"", s)
	}
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (supported: png, jpeg, gif)", path, err)
	}
	_ = format
	return img, nil
}

// makeSquare returns a square image, either padded or centre-cropped. An image
// that is already square is returned untouched.
func makeSquare(src image.Image, mode fitMode, bg color.Color) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return src
	}

	if mode == modeCrop {
		side := min(w, h)
		// Centre the crop window on the source.
		x := b.Min.X + (w-side)/2
		y := b.Min.Y + (h-side)/2
		dst := image.NewNRGBA(image.Rect(0, 0, side, side))
		draw.Draw(dst, dst.Bounds(), src, image.Pt(x, y), draw.Src)
		return dst
	}

	side := max(w, h)
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	// A fully transparent background needs no fill; anything else does.
	if _, _, _, a := bg.RGBA(); a != 0 {
		draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}
	at := image.Pt((side-w)/2, (side-h)/2)
	draw.Draw(dst, image.Rect(at.X, at.Y, at.X+w, at.Y+h), src, b.Min, draw.Over)
	return dst
}

// resize scales a square image to side x side. CatmullRom is worth the extra
// work here: icons are shrunk enormously (256 down to 16) and a cheaper kernel
// turns fine detail into mush.
func resize(src image.Image, side int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// parseColor accepts "transparent", "#rgb", "#rrggbb" or "#rrggbbaa".
func parseColor(s string) (color.Color, error) {
	if s == "" || s == "transparent" || s == "none" {
		return color.NRGBA{}, nil
	}
	hex := s
	if hex[0] == '#' {
		hex = hex[1:]
	}
	var r, g, b, a uint8 = 0, 0, 0, 0xff
	switch len(hex) {
	case 3:
		if _, err := fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b); err != nil {
			return nil, fmt.Errorf("bad colour %q", s)
		}
		r, g, b = r*17, g*17, b*17
	case 6:
		if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
			return nil, fmt.Errorf("bad colour %q", s)
		}
	case 8:
		if _, err := fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a); err != nil {
			return nil, fmt.Errorf("bad colour %q", s)
		}
	default:
		return nil, fmt.Errorf("bad colour %q; use \"transparent\", \"#rgb\", \"#rrggbb\" or \"#rrggbbaa\"", s)
	}
	return color.NRGBA{R: r, G: g, B: b, A: a}, nil
}
