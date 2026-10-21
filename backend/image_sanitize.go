package main

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var imageDecodeSlots = make(chan struct{}, 2)

func sanitizeImage(data []byte, static bool) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the image is damaged or cannot be decoded")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8000 || cfg.Height > 8000 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, errors.New("image dimensions are too large; use an image below 16 megapixels and 8000 pixels per side")
	}
	if !static {
		return data, nil
	} // animated banners retain their frames
	select {
	case imageDecodeSlots <- struct{}{}:
		defer func() { <-imageDecodeSlots }()
	default:
		return nil, errors.New("image processing is busy; wait a moment and upload again")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the image is damaged or cannot be decoded")
	}
	scale := math.Min(1, 2000/float64(max(cfg.Width, cfg.Height)))
	target := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(cfg.Width)*scale)), max(1, int(float64(cfg.Height)*scale))))
	draw.CatmullRom.Scale(target, target.Bounds(), img, img.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if target.Opaque() {
		err = jpeg.Encode(&out, target, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&out, target)
	}
	if err != nil {
		return nil, errors.New("could not process image")
	}
	if int64(out.Len()) > maxPhoto {
		return nil, errors.New("processed image is too large; choose a smaller image")
	}
	return out.Bytes(), nil
}
