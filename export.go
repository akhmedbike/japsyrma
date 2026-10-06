package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"japsyrma/internal/imaging"
)

// monoFromRequest — 1-битная этикетка ровно W×H точек, как для печати.
func monoFromRequest(r PrintRequest) (*imaging.Bitmap, error) {
	if r.WidthMM <= 0 || r.HeightMM <= 0 {
		return nil, errors.New("не задан размер этикетки")
	}
	raw, err := base64.StdEncoding.DecodeString(r.PNGBase64)
	if err != nil {
		return nil, fmt.Errorf("изображение этикетки: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("изображение этикетки: %w", err)
	}
	return imaging.ToGray(img, dots(r.WidthMM), dots(r.HeightMM)).Threshold(128), nil
}

// ---------- PNG ----------

// encodePNG пишет 1-битный PNG с отметкой 203 dpi (чанк pHYs),
// чтобы при печати из просмотрщика сохранялся натуральный размер.
func encodePNG(b *imaging.Bitmap) ([]byte, error) {
	pal := color.Palette{color.Black, color.White}
	img := image.NewPaletted(image.Rect(0, 0, b.W, b.H), pal)
	for i, blk := range b.Black {
		if !blk {
			img.Pix[i] = 1
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	data := buf.Bytes()

	ppm := uint32(math.Round(dpi / 0.0254)) // точек на метр
	chunk := make([]byte, 0, 21)
	chunk = binary.BigEndian.AppendUint32(chunk, 9)
	body := []byte("pHYs")
	body = binary.BigEndian.AppendUint32(body, ppm)
	body = binary.BigEndian.AppendUint32(body, ppm)
	body = append(body, 1) // единица — метр
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))

	const afterIHDR = 8 + 25 // сигнатура + IHDR
	out := make([]byte, 0, len(data)+len(chunk))
	out = append(out, data[:afterIHDR]...)
	out = append(out, chunk...)
	out = append(out, data[afterIHDR:]...)
	return out, nil
}

// ---------- PDF ----------

// encodePDF — одна страница размером с этикетку, внутри 1-битное изображение.
// Без сторонних библиотек: PDF здесь очень простой.
func encodePDF(b *imaging.Bitmap, widthMM, heightMM float64) ([]byte, error) {
	// в PDF DeviceGray 1 бит: 0 — черный, 1 — белый
	_, raw := b.Pack(false)
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	pw := widthMM * 72 / 25.4
	ph := heightMM * 72 / 25.4
	content := fmt.Sprintf("q %.3f 0 0 %.3f 0 0 cm /Im0 Do Q\n", pw, ph)

	var out bytes.Buffer
	offsets := make([]int, 6)
	obj := func(n int, s string) {
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, s)
	}

	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.3f %.3f] "+
		"/Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", pw, ph))

	offsets[4] = out.Len()
	fmt.Fprintf(&out, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
		"/ColorSpace /DeviceGray /BitsPerComponent 1 /Interpolate false "+
		"/Filter /FlateDecode /Length %d >>\nstream\n", b.W, b.H, z.Len())
	out.Write(z.Bytes())
	out.WriteString("\nendstream\nendobj\n")

	offsets[5] = out.Len()
	fmt.Fprintf(&out, "5 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(content), content)

	xref := out.Len()
	out.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return out.Bytes(), nil
}

// ---------- методы для фронтенда ----------

func encode(r PrintRequest, format string) ([]byte, error) {
	b, err := monoFromRequest(r)
	if err != nil {
		return nil, err
	}
	switch format {
	case "png":
		return encodePNG(b)
	case "pdf":
		return encodePDF(b, r.WidthMM, r.HeightMM)
	}
	return nil, fmt.Errorf("неизвестный формат %q", format)
}

// ExportFile сохраняет этикетку в PNG или PDF в натуральном размере.
func (a *App) ExportFile(r PrintRequest, format string) (string, error) {
	data, err := encode(r, format)
	if err != nil {
		return "", err
	}
	filter := runtime.FileFilter{DisplayName: "Изображение PNG (*.png)", Pattern: "*.png"}
	if format == "pdf" {
		filter = runtime.FileFilter{DisplayName: "Документ PDF (*.pdf)", Pattern: "*.pdf"}
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Экспорт этикетки",
		DefaultFilename: "этикетка." + format,
		Filters:         []runtime.FileFilter{filter},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// ProofPDF создает временный PDF и открывает его в программе по умолчанию,
// чтобы напечатать пробный оттиск на обычном принтере.
func (a *App) ProofPDF(r PrintRequest) error {
	data, err := encode(r, "pdf")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "proof-*.pdf")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return openWithDefaultApp(f.Name())
}

func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
