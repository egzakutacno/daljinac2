package screen

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"sync"

	"github.com/kbinani/screenshot"
	"golang.org/x/image/draw"
)

type CaptureOpts struct {
	MaxWidth  int
	Quality   int
	MonitorID int
}

var monitorCache struct {
	sync.Mutex
	count int
}

func NumMonitors() int {
	return screenshot.NumActiveDisplays()
}

func Capture(monitorID int) (*image.RGBA, error) {
	n := screenshot.NumActiveDisplays()
	if monitorID >= n {
		monitorID = 0
	}
	bounds := screenshot.GetDisplayBounds(monitorID)
	return screenshot.CaptureRect(bounds)
}

func CaptureAll() ([]*image.RGBA, error) {
	n := screenshot.NumActiveDisplays()
	imgs := make([]*image.RGBA, 0, n)
	for i := 0; i < n; i++ {
		img, err := Capture(i)
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, img)
	}
	return imgs, nil
}

func resize(img *image.RGBA, maxWidth int) *image.RGBA {
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= maxWidth {
		return img
	}
	ratio := float64(maxWidth) / float64(w)
	newW := maxWidth
	newH := int(float64(h) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func EncodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

func EncodeJPEG(img *image.RGBA, quality int) ([]byte, error) {
	if quality <= 0 {
		quality = 80
	}
	if quality > 100 {
		quality = 100
	}
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes(), err
}

func CapturePNG(monitorID int, maxWidth int) ([]byte, error) {
	img, err := Capture(monitorID)
	if err != nil {
		return nil, err
	}
	if maxWidth > 0 {
		img = resize(img, maxWidth)
	}
	return EncodePNG(img)
}

func CaptureJPEG(monitorID int, maxWidth int, quality int) ([]byte, error) {
	img, err := Capture(monitorID)
	if err != nil {
		return nil, err
	}
	if maxWidth > 0 {
		img = resize(img, maxWidth)
	}
	return EncodeJPEG(img, quality)
}
