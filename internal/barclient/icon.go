package barclient

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

func IconPNG(state IconState) ([]byte, error) {
	canvas := image.NewNRGBA(image.Rect(0, 0, 18, 18))
	ink := color.NRGBA{R: 55, G: 61, B: 66, A: 255}
	switch state {
	case IconAttention:
		ink = color.NRGBA{R: 232, G: 163, B: 61, A: 255}
	case IconRust:
		ink = color.NRGBA{R: 217, G: 105, B: 90, A: 255}
	case IconTemplate:
		ink = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	}
	for y := 3; y < 15; y++ {
		for x := 3; x < 15; x++ {
			if x == 3 || x == 14 || y == 3 || y == 14 || x == y || x+y == 17 {
				canvas.SetNRGBA(x, y, ink)
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
