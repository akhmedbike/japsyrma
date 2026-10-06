// Package tspl формирует поток команд TSPL/TSPL2 для принтеров этикеток TSC.
package tspl

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Media — способ, которым принтер определяет границы этикетки.
type Media int

const (
	MediaGap        Media = iota // этикетки разделены зазором
	MediaBlackMark               // черная метка на обороте подложки
	MediaContinuous              // непрерывная лента
)

// Setup — настройки задания.
type Setup struct {
	WidthMM, HeightMM  float64
	GapMM, GapOffsetMM float64
	Media              Media
	Density            int // 0..15, -1 = не менять настройку принтера
	Speed              int // дюймов/с, 0 = не менять
	Direction          int // 0 или 1
	RefX, RefY         int // смещение точки отсчета, в точках
}

// Job накапливает команды TSPL.
type Job struct {
	buf bytes.Buffer
}

func mm(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " mm" }

// NewJob записывает команды настройки.
func NewJob(s Setup) *Job {
	j := &Job{}
	j.Cmd("SIZE %s, %s", mm(s.WidthMM), mm(s.HeightMM))
	switch s.Media {
	case MediaBlackMark:
		j.Cmd("BLINE %s, %s", mm(s.GapMM), mm(s.GapOffsetMM))
	case MediaContinuous:
		j.Cmd("GAP 0 mm, 0 mm")
	default:
		j.Cmd("GAP %s, %s", mm(s.GapMM), mm(s.GapOffsetMM))
	}
	j.Cmd("DIRECTION %d", s.Direction)
	j.Cmd("REFERENCE %d,%d", s.RefX, s.RefY)
	if s.Density >= 0 {
		j.Cmd("DENSITY %d", s.Density)
	}
	if s.Speed > 0 {
		j.Cmd("SPEED %d", s.Speed)
	}
	return j
}

// Cmd добавляет произвольную команду с переводом строки CRLF.
func (j *Job) Cmd(format string, args ...any) *Job {
	fmt.Fprintf(&j.buf, format, args...)
	j.buf.WriteString("\r\n")
	return j
}

// Cls очищает буфер изображения принтера.
func (j *Job) Cls() *Job { return j.Cmd("CLS") }

// Bar — залитый черный прямоугольник.
func (j *Job) Bar(x, y, w, h int) *Job { return j.Cmd("BAR %d,%d,%d,%d", x, y, w, h) }

// Box — рамка.
func (j *Job) Box(x1, y1, x2, y2, thickness int) *Job {
	return j.Cmd("BOX %d,%d,%d,%d,%d", x1, y1, x2, y2, thickness)
}

// Text печатает текст встроенным шрифтом принтера. Только ASCII:
// кириллицу рисуем сами в bitmap.
func (j *Job) Text(x, y int, font string, rotation, xmul, ymul int, s string) *Job {
	s = strings.ReplaceAll(s, `"`, `\["]`)
	return j.Cmd(`TEXT %d,%d,"%s",%d,%d,%d,"%s"`, x, y, font, rotation, xmul, ymul, s)
}

// Bitmap отправляет упакованное 1-битное изображение
// (ширина widthBytes*8 точек, height строк). Режим 0 = OVERWRITE.
func (j *Job) Bitmap(x, y, widthBytes, height int, data []byte) *Job {
	fmt.Fprintf(&j.buf, "BITMAP %d,%d,%d,%d,0,", x, y, widthBytes, height)
	j.buf.Write(data)
	j.buf.WriteString("\r\n")
	return j
}

// Print печатает sets наборов по copies копий.
func (j *Job) Print(sets, copies int) *Job { return j.Cmd("PRINT %d,%d", sets, copies) }

// FormFeed прогоняет одну пустую этикетку.
func (j *Job) FormFeed() *Job { return j.Cmd("FORMFEED") }

// Bytes возвращает готовое задание.
func (j *Job) Bytes() []byte { return j.buf.Bytes() }
