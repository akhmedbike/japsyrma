// Package imaging: загрузка изображений, перевод в оттенки серого,
// дизеринг и упаковка в 1-битный растр для термопринтера.
package imaging

import (
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
)

// Bitmap — 1-битный растр, true = черная точка.
type Bitmap struct {
	W, H  int
	Black []bool
}

func New(w, h int) *Bitmap { return &Bitmap{W: w, H: h, Black: make([]bool, w*h)} }

func (b *Bitmap) Set(x, y int, black bool) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return
	}
	b.Black[y*b.W+x] = black
}

func (b *Bitmap) FillRect(x, y, w, h int, black bool) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			b.Set(xx, yy, black)
		}
	}
}

// Draw накладывает черные точки src в позицию (ox, oy).
func (b *Bitmap) Draw(src *Bitmap, ox, oy int) {
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			if src.Black[y*src.W+x] {
				b.Set(ox+x, oy+y, true)
			}
		}
	}
}

// Pack упаковывает строки, старший бит первым. В TSPL BITMAP бит 0 обычно
// означает черную точку; если печать вышла негативом, передайте blackIsOne=true.
func (b *Bitmap) Pack(blackIsOne bool) (widthBytes int, data []byte) {
	widthBytes = (b.W + 7) / 8
	data = make([]byte, widthBytes*b.H)
	for y := 0; y < b.H; y++ {
		row := data[y*widthBytes : (y+1)*widthBytes]
		for x := 0; x < widthBytes*8; x++ {
			black := x < b.W && b.Black[y*b.W+x]
			if black == blackIsOne {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return widthBytes, data
}

// Image возвращает превью для сохранения в PNG.
func (b *Bitmap) Image() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, b.W, b.H))
	for i, blk := range b.Black {
		if blk {
			img.Pix[i] = 0
		} else {
			img.Pix[i] = 255
		}
	}
	return img
}

// Load открывает PNG или JPG.
func Load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// Fit — наибольший размер с пропорциями src, влезающий в maxW×maxH.
func Fit(srcW, srcH, maxW, maxH int) (int, int) {
	s := math.Min(float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
	w := int(math.Round(float64(srcW) * s))
	h := int(math.Round(float64(srcH) * s))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// Gray — яркость 0 (черный) .. 255 (белый).
type Gray struct {
	W, H int
	V    []float64
}

func luma(c color.Color) float64 {
	r, g, b, a := c.RGBA() // премультиплицированные, 0..0xffff
	white := float64(0xffff - a) // прозрачность -> на белый фон
	l := 0.299*(float64(r)+white) + 0.587*(float64(g)+white) + 0.114*(float64(b)+white)
	return l / 257.0
}

// ToGray масштабирует img до w×h усреднением по площади.
func ToGray(img image.Image, w, h int) *Gray {
	sb := img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	g := &Gray{W: w, H: h, V: make([]float64, w*h)}
	for y := 0; y < h; y++ {
		sy0 := sb.Min.Y + y*sh/h
		sy1 := sb.Min.Y + (y+1)*sh/h
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0 := sb.Min.X + x*sw/w
			sx1 := sb.Min.X + (x+1)*sw/w
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			sum, n := 0.0, 0
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					sum += luma(img.At(sx, sy))
					n++
				}
			}
			g.V[y*w+x] = sum / float64(n)
		}
	}
	return g
}

func clamp(v float64) float64 { return math.Max(0, math.Min(255, v)) }

// Adjust применяет яркость (-255..255) и контраст (множитель, 1 = без изменений).
func (g *Gray) Adjust(brightness, contrast float64) {
	for i, v := range g.V {
		g.V[i] = clamp((v-128)*contrast + 128 + brightness)
	}
}

// Threshold: темнее порога — черное.
func (g *Gray) Threshold(t float64) *Bitmap {
	b := New(g.W, g.H)
	for i, v := range g.V {
		b.Black[i] = v < t
	}
	return b
}

// Dither — Floyd–Steinberg, для фото и градиентов.
func (g *Gray) Dither() *Bitmap {
	v := append([]float64(nil), g.V...)
	b := New(g.W, g.H)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			i := y*g.W + x
			old := v[i]
			nw := 255.0
			if old < 128 {
				nw = 0
				b.Black[i] = true
			}
			e := old - nw
			if x+1 < g.W {
				v[i+1] += e * 7 / 16
			}
			if y+1 < g.H {
				if x > 0 {
					v[i+g.W-1] += e * 3 / 16
				}
				v[i+g.W] += e * 5 / 16
				if x+1 < g.W {
					v[i+g.W+1] += e / 16
				}
			}
		}
	}
	return b
}
