package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"japsyrma/internal/imaging"
	"japsyrma/internal/tspl"
)

const dpi = 203
const maxWidthMM = 108

type App struct {
	ctx       context.Context
	lastPrint *PrintRecord
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// На экранах меньше окна по умолчанию (например, нетбук 1280×720)
	// заголовок уходит за верх экрана: окно нельзя ни сдвинуть, ни закрыть.
	// Тогда подгоняем нормальный размер под экран и разворачиваем окно.
	if screens, err := runtime.ScreenGetAll(ctx); err == nil {
		for _, s := range screens {
			if !s.IsCurrent {
				continue
			}
			if s.Size.Width < 1320 || s.Size.Height < 880 {
				runtime.WindowSetSize(ctx, min(1280, s.Size.Width-40), min(820, s.Size.Height-120))
				runtime.WindowCenter(ctx)
				runtime.WindowMaximise(ctx)
			}
			break
		}
	}
}

func dots(mm float64) int { return int(math.Round(mm * dpi / 25.4)) }

// ---------- принтеры ----------

type PrinterList struct {
	Names   []string `json:"names"`
	Default string   `json:"default"`
}

func (a *App) ListPrinters() (PrinterList, error) {
	names, def, err := listPrinters()
	if err != nil {
		return PrinterList{}, err
	}
	return PrinterList{Names: names, Default: def}, nil
}

// ---------- печать ----------

// PrintRequest приходит из фронтенда. PNGBase64 — уже 1-битная картинка
// размером ровно с этикетку в точках принтера.
type PrintRequest struct {
	Printer   string  `json:"printer"`
	PNGBase64 string  `json:"pngBase64"`
	WidthMM   float64 `json:"widthMM"`
	HeightMM  float64 `json:"heightMM"`
	GapMM     float64 `json:"gapMM"`
	Media     string  `json:"media"`
	Density   int     `json:"density"`
	Speed     int     `json:"speed"`
	Direction int     `json:"direction"`
	Copies    int     `json:"copies"`
	Invert    bool    `json:"invert"`
}

func (r PrintRequest) setup() tspl.Setup {
	m := tspl.MediaGap
	switch r.Media {
	case "bline":
		m = tspl.MediaBlackMark
	case "cont":
		m = tspl.MediaContinuous
	}
	return tspl.Setup{
		WidthMM: r.WidthMM, HeightMM: r.HeightMM,
		GapMM: r.GapMM, Media: m,
		Density: r.Density, Speed: r.Speed, Direction: r.Direction,
	}
}

func (r PrintRequest) validate() error {
	if r.WidthMM <= 0 || r.HeightMM <= 0 {
		return errors.New("не задан размер этикетки")
	}
	if r.WidthMM > maxWidthMM {
		return fmt.Errorf("ширина %.1f мм больше максимальной ширины печати %d мм", r.WidthMM, maxWidthMM)
	}
	return nil
}

func buildJob(r PrintRequest) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(r.PNGBase64)
	if err != nil {
		return nil, fmt.Errorf("изображение этикетки: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("изображение этикетки: %w", err)
	}
	W, H := dots(r.WidthMM), dots(r.HeightMM)
	// При совпадении размеров ToGray работает 1:1, порог переводит в 1 бит.
	bmp := imaging.ToGray(img, W, H).Threshold(128)
	wb, data := bmp.Pack(r.Invert)

	copies := r.Copies
	if copies < 1 {
		copies = 1
	}
	job := tspl.NewJob(r.setup())
	job.Cls().Bitmap(0, 0, wb, H, data).Print(1, copies)
	return job.Bytes(), nil
}

func (a *App) Print(r PrintRequest) (string, error) {
	data, err := buildJob(r)
	if err != nil {
		a.lastPrint = &PrintRecord{Time: time.Now(), Printer: r.Printer, Error: err.Error()}
		return "", err
	}
	id, err := sendRaw(r.Printer, data)
	rec := &PrintRecord{Time: time.Now(), Printer: r.Printer, Bytes: len(data)}
	if err != nil {
		rec.Error = err.Error()
	}
	a.lastPrint = rec
	if err != nil {
		return "", err
	}
	if id != 0 {
		go a.watchJob(r.Printer, id)
	}
	return fmt.Sprintf("Отправлено на печать: %d шт.", max(1, r.Copies)), nil
}

// FeedLabel прогоняет одну пустую этикетку — проверка связи и датчика зазора.
func (a *App) FeedLabel(r PrintRequest) error {
	if err := r.validate(); err != nil {
		return err
	}
	id, err := sendRaw(r.Printer, tspl.NewJob(r.setup()).FormFeed().Bytes())
	a.lastPrint = &PrintRecord{Time: time.Now(), Printer: r.Printer, Error: errString(err)}
	if err != nil {
		return err
	}
	if id != 0 {
		go a.watchJob(r.Printer, id)
	}
	return nil
}

// ---------- диагностика печати ----------

// PrintRecord — что произошло при последней попытке печати.
type PrintRecord struct {
	Time    time.Time `json:"time"`
	Printer string    `json:"printer"`
	Bytes   int       `json:"bytes"`
	Error   string    `json:"error,omitempty"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// DiagResult — один шаг проверки: ok | warn | error | info.
type DiagResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// SpoolerState — снимок состояния принтера в спулере (GetPrinter level 2).
type SpoolerState struct {
	Driver         string
	Port           string
	PrintProcessor string
	WorkOffline    bool
	StatusOffline  bool
	StatusText     string
}

// jobSummary — задание в очереди печати.
type jobSummary struct {
	ID         uint32
	Document   string
	Status     string
	StatusCode uint32
	Position   uint32
}

// Diagnose пошагово проверяет путь задания до принтера и рассказывает,
// где оно застряло. Срабатывает по кнопке «Диагностика» в панели печати.
func (a *App) Diagnose(printerName string) []DiagResult {
	out := []DiagResult{}
	add := func(name, status, detail string) {
		out = append(out, DiagResult{Name: name, Status: status, Detail: detail})
	}

	names, def, err := listPrinters()
	if err != nil {
		add("Список принтеров", "error", err.Error())
		return out
	}
	add("Список принтеров", "ok",
		fmt.Sprintf("найдено %d, по умолчанию: %s", len(names), orDash(def)))

	if printerName == "" {
		add("Выбранный принтер", "error", "принтер не выбран")
		return out
	}
	if !slices.Contains(names, printerName) {
		add("Выбранный принтер", "error",
			fmt.Sprintf("%q отсутствует в списке принтеров — переустановите драйвер или выберите другой", printerName))
		return out
	}
	add("Выбранный принтер", "ok", printerName)

	if st, err := readSpooler(printerName); err != nil {
		add("Спулер", "info", err.Error()+" — шаг пропущен")
	} else {
		add("Драйвер и порт", "ok",
			fmt.Sprintf("%s / %s", orDash(st.Driver), orDash(st.Port)))
		switch {
		case st.WorkOffline:
			add("Режим работы", "error",
				"принтер переведен в «Работать автономно» — снимите галочку в окне очереди печати")
		case st.StatusOffline:
			add("Режим работы", "error", "спулер считает принтер офлайн (нет связи по порту)")
		case st.StatusText != "":
			add("Режим работы", "warn", st.StatusText)
		}
	}

	if jobs, err := queueSnapshot(printerName); err != nil {
		add("Очередь печати", "info", "не удалось прочитать: "+err.Error())
	} else if len(jobs) > 0 {
		detail := fmt.Sprintf("заданий: %d", len(jobs))
		if j := jobs[0]; j.Document != "" || j.Status != "" {
			detail += fmt.Sprintf(" (№%d «%s», %s)", j.ID, j.Document, orDash(j.Status))
		}
		detail += " — отмените все кнопкой ниже или очистите окно очереди"
		add("Очередь печати", "warn", detail)
	} else {
		add("Очередь печати", "ok", "пуста")
	}

	if err := probeRaw(printerName); err != nil {
		add("Пробное RAW-задание", "error", err.Error())
	} else {
		add("Пробное RAW-задание", "ok", "весь путь пройден: задание создано и закрыто")
	}

	if rec := a.lastPrint; rec != nil {
		if rec.Error != "" {
			add("Последняя попытка", "error", rec.Time.Format("15:04:05")+": "+rec.Error)
		} else {
			add("Последняя попытка", "ok",
				fmt.Sprintf("%s — ушла в спулер, %d байт", rec.Time.Format("15:04:05"), rec.Bytes))
		}
	} else {
		add("Последняя попытка", "info", "в этом запуске печати еще не было")
	}
	return out
}

// OpenPrintQueue открывает окно очереди печати Windows выбранного принтера.
func (a *App) OpenPrintQueue(printerName string) error {
	if printerName == "" {
		return errors.New("принтер не выбран")
	}
	return openPrintQueue(printerName)
}

// CancelAllJobs удаляет все задания из очереди выбранного принтера.
func (a *App) CancelAllJobs(printerName string) (int, error) {
	if printerName == "" {
		return 0, errors.New("принтер не выбран")
	}
	return cancelAllJobs(printerName)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ExportPRN сохраняет задание в файл — удобно для отладки без принтера.
func (a *App) ExportPRN(r PrintRequest) (string, error) {
	data, err := buildJob(r)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Сохранить задание печати",
		DefaultFilename: "label.prn",
		Filters:         []runtime.FileFilter{{DisplayName: "Задание TSPL (*.prn)", Pattern: "*.prn"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// ---------- изображения ----------

var imageFilter = []runtime.FileFilter{{
	DisplayName: "Изображения (*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp;*.svg)",
	Pattern:     "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp;*.svg",
}}

func imageMime(path string, head []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp":
		if ct := http.DetectContentType(head); strings.HasPrefix(ct, "image/") {
			return ct
		}
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	return ""
}

// OpenImage открывает нативный диалог выбора картинки и отдает её как data URL.
// Пустая строка — пользователь отменил выбор. Нативный диалог вместо <input type="file">
// обходит капризы файловых диалогов WebView2 на Windows.
func (a *App) OpenImage() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Выберите изображение",
		Filters: imageFilter,
	})
	if err != nil || path == "" {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mime := imageMime(path, b)
	if mime == "" {
		return "", fmt.Errorf("файл не похож на изображение: %s", filepath.Base(path))
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// ---------- шаблоны ----------

type TemplateFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

var labelFilter = []runtime.FileFilter{{DisplayName: "Шаблон этикетки (*.label)", Pattern: "*.label"}}

// SaveTemplate пишет JSON шаблона. Пустой path — спросить у пользователя.
// Возвращает путь или пустую строку, если пользователь отменил диалог.
func (a *App) SaveTemplate(content, path string) (string, error) {
	if path == "" {
		p, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Сохранить шаблон",
			DefaultFilename: "этикетка.label",
			Filters:         labelFilter,
		})
		if err != nil || p == "" {
			return "", err
		}
		path = p
	}
	if !strings.EqualFold(filepath.Ext(path), ".label") {
		path += ".label"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) OpenTemplate() (*TemplateFile, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Открыть шаблон",
		Filters: labelFilter,
	})
	if err != nil || path == "" {
		return nil, err
	}
	return a.LoadTemplate(path)
}

// LoadTemplate читает шаблон по конкретному пути — для «Открыть последние».
func (a *App) LoadTemplate(path string) (*TemplateFile, error) {
	if path == "" {
		return nil, errors.New("не задан путь к шаблону")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &TemplateFile{Path: path, Content: string(b)}, nil
}
