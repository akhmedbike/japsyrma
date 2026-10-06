//go:build windows

package main

// Печать через спулер Windows. Основной путь — библиотека
// alexbrainman/printer; прямые вызовы winspool.drv добавлены только для того,
// чего в библиотеке нет: GetPrinter (драйвер/порт/статус) и SetJob (отмена заданий).

import (
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/alexbrainman/printer"
)

func listPrinters() ([]string, string, error) {
	names, err := printer.ReadNames()
	if err != nil {
		return nil, "", err
	}
	def, _ := printer.Default()
	return names, def, nil
}

// ---------- прямые вызовы winspool ----------

var (
	modWinspool      = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinter  = modWinspool.NewProc("OpenPrinterW")
	procClosePrinter = modWinspool.NewProc("ClosePrinter")
	procGetPrinterW  = modWinspool.NewProc("GetPrinterW")
	procSetJobW      = modWinspool.NewProc("SetJobW")
)

const (
	jobControlDelete = 5

	attrWorkOffline = 0x400  // PRINTER_ATTRIBUTE_WORK_OFFLINE («работать автономно»)
	statusOffline   = 0x80   // PRINTER_STATUS_OFFLINE
)

// PRINTER_INFO_2W, выравнивание проверено для amd64.
type printerInfo2 struct {
	ServerName, PrinterName, ShareName, PortName, DriverName, Comment, Location *uint16
	DevMode                                                                    uintptr
	SepFile, PrintProcessor, Datatype, Parameters                              *uint16
	SecurityDescriptor                                                         uintptr
	Attributes, Priority, DefaultPriority, StartTime, UntilTime                uint32
	Status, Jobs, AveragePPM                                                   uint32
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func fromUTF16(p *uint16) string {
	if p == nil {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(p, utf16Len(p)))
}

func utf16Len(p *uint16) int {
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*uintptr(n))) != 0 {
		n++
	}
	return n
}

func openSpooler(name string) (syscall.Handle, error) {
	var h syscall.Handle
	r, _, e := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(utf16(name))), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return 0, fmt.Errorf("открыть %q: %v", name, e)
	}
	return h, nil
}

func closeSpooler(h syscall.Handle) { procClosePrinter.Call(uintptr(h)) }

// printerStatusText переводит биты PRINTER_STATUS_* в человеческий список.
func printerStatusText(s uint32) string {
	var parts []string
	if s&0x8 != 0 {
		parts = append(parts, "замятие бумаги")
	}
	if s&0x10 != 0 {
		parts = append(parts, "нет бумаги")
	}
	if s&0x40 != 0 {
		parts = append(parts, "проблема бумаги")
	}
	if s&0x80 != 0 {
		parts = append(parts, "офлайн")
	}
	if s&0x100000 != 0 {
		parts = append(parts, "требуется вмешательство")
	}
	if s&0x400000 != 0 {
		parts = append(parts, "открыта крышка")
	}
	if s&0x20000 != 0 {
		parts = append(parts, "мало тонера/ленты")
	}
	return strings.Join(parts, ", ")
}

// readSpooler опрашивает драйвер, порт и состояние напрямую через GetPrinter(2) —
// без PowerShell/WMI, поэтому работает быстро и не зависит от политики исполнения скриптов.
func readSpooler(name string) (*SpoolerState, error) {
	h, err := openSpooler(name)
	if err != nil {
		return nil, err
	}
	defer closeSpooler(h)

	var need uint32
	procGetPrinterW.Call(uintptr(h), 2, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		return nil, errors.New("GetPrinter: пустой ответ спулера")
	}
	buf := make([]byte, need)
	r, _, e := procGetPrinterW.Call(uintptr(h), 2,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return nil, fmt.Errorf("GetPrinter: %v", e)
	}
	pi := (*printerInfo2)(unsafe.Pointer(&buf[0]))
	st := &SpoolerState{
		Driver:         fromUTF16(pi.DriverName),
		Port:           fromUTF16(pi.PortName),
		PrintProcessor: fromUTF16(pi.PrintProcessor),
		WorkOffline:    pi.Attributes&attrWorkOffline != 0,
		StatusOffline:  pi.Status&statusOffline != 0,
		StatusText:     printerStatusText(pi.Status),
	}
	goruntime.KeepAlive(buf)
	return st, nil
}

// queueSnapshot — текущие задания принтера через EnumJobs.
func queueSnapshot(name string) ([]jobSummary, error) {
	p, err := printer.Open(name)
	if err != nil {
		return nil, fmt.Errorf("открыть %q: %w", name, err)
	}
	defer p.Close()
	jobs, err := p.Jobs()
	if err != nil {
		return nil, err
	}
	out := make([]jobSummary, len(jobs))
	for i, j := range jobs {
		out[i] = jobSummary{ID: j.JobID, Document: j.DocumentName, Status: j.Status, StatusCode: j.StatusCode, Position: j.Position}
	}
	return out, nil
}

// cancelAllJobs удаляет все задания из очереди принтера.
func cancelAllJobs(name string) (int, error) {
	jobs, err := queueSnapshot(name)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	h, err := openSpooler(name)
	if err != nil {
		return 0, err
	}
	defer closeSpooler(h)

	n := 0
	var failed []string
	for _, j := range jobs {
		r, _, e := procSetJobW.Call(uintptr(h), uintptr(j.ID), 0, 0, jobControlDelete)
		if r == 0 {
			failed = append(failed, fmt.Sprintf("№%d: %v", j.ID, e))
			continue
		}
		n++
	}
	if len(failed) > 0 {
		return n, fmt.Errorf("не удалились: %s (чужие задания удаляются только с правами администратора)",
			strings.Join(failed, "; "))
	}
	return n, nil
}

// ---------- печать ----------

// startDocument создаёт задание с типом данных RAW. XPS-драйверам v4 может
// подходить XPS_PASS, поэтому при неудаче пробуем и его. Библиотечный
// StartRawDocument не используем: он заранее опрашивает драйвер через
// GetPrinterDriver(level 8), и этот вызов на части драйверов (в т.ч. TSC)
// падает с «The data is invalid» ещё до создания задания.
func startDocument(p *printer.Printer, docName string) error {
	errRaw := p.StartDocument(docName, "RAW")
	if errRaw == nil {
		return nil
	}
	if errXps := p.StartDocument(docName, "XPS_PASS"); errXps == nil {
		return nil
	} else {
		return fmt.Errorf("RAW: %v; XPS_PASS: %v", errRaw, errXps)
	}
}

// sendRaw отправляет байты через спулер Windows с типом данных RAW и возвращает
// номер созданного задания — по нему можно следить, дошло ли оно до принтера.
// Каждый шаг обёрнут именем, чтобы ошибка говорила, где именно сломалось.
func sendRaw(name string, data []byte) (uint32, error) {
	if name == "" {
		def, err := printer.Default()
		if err != nil {
			return 0, fmt.Errorf("принтер по умолчанию: %w", err)
		}
		name = def
	}
	p, err := printer.Open(name)
	if err != nil {
		return 0, fmt.Errorf("открыть %q: %w", name, err)
	}
	defer p.Close()

	docName := fmt.Sprintf("japsyrma-%d", time.Now().UnixMilli())
	if err := startDocument(p, docName); err != nil {
		return 0, fmt.Errorf("создание задания: %w", err)
	}
	if err := p.StartPage(); err != nil {
		p.EndDocument()
		return 0, fmt.Errorf("начало страницы: %w", err)
	}
	n, err := p.Write(data)
	if err != nil {
		p.EndPage()
		p.EndDocument()
		return 0, fmt.Errorf("отправка данных (%d Б): %w", len(data), err)
	}
	if n != len(data) {
		p.EndPage()
		p.EndDocument()
		return 0, fmt.Errorf("отправка данных: записано %d из %d байт", n, len(data))
	}
	if err := p.EndPage(); err != nil {
		p.EndDocument()
		return 0, fmt.Errorf("конец страницы: %w", err)
	}
	if err := p.EndDocument(); err != nil {
		return 0, fmt.Errorf("закрытие задания: %w", err)
	}

	// Задание уже в очереди — найдём его номер по имени документа.
	// Если не нашли (маленькое задание успело уйти), слежение просто не начнётся.
	id := uint32(0)
	if jobs, err := p.Jobs(); err == nil {
		for _, j := range jobs {
			if j.DocumentName == docName {
				id = j.JobID
				break
			}
		}
	}
	return id, nil
}

// probeRaw прогоняет пустое задание через весь путь спулера
// (создание → страница → закрытие), ничего не печатая, и называет
// шаг, на котором путь ломается.
func probeRaw(name string) error {
	p, err := printer.Open(name)
	if err != nil {
		return fmt.Errorf("открыть %q: %w", name, err)
	}
	defer p.Close()
	if err := startDocument(p, "japsyrma-проба"); err != nil {
		return fmt.Errorf("создание задания: %w", err)
	}
	if err := p.StartPage(); err != nil {
		p.EndDocument()
		return fmt.Errorf("начало страницы: %w", err)
	}
	if err := p.EndPage(); err != nil {
		p.EndDocument()
		return fmt.Errorf("конец страницы: %w", err)
	}
	if err := p.EndDocument(); err != nil {
		return fmt.Errorf("закрытие задания: %w", err)
	}
	return nil
}

// ---------- слежение за заданием ----------

// jobWatchEvent уходит во фронтенд событием «print:watch».
type jobWatchEvent struct {
	ID    uint32 `json:"id"`
	State string `json:"state"` // progress | done | stuck | error
	Text  string `json:"text"`
}

// Биты JOB_STATUS_*, означающие проблему.
const (
	jobStError        = 0x2
	jobStOffline      = 0x20
	jobStPaperOut     = 0x40
	jobStBlocked      = 0x200
	jobStIntervention = 0x400
)

// watchJob следит за заданием в очереди: успех печати означает лишь, что
// спулер принял данные, а дошли ли они до порта принтера — видно только здесь.
func (a *App) watchJob(name string, id uint32) {
	if a.ctx == nil {
		return
	}
	emit := func(state, text string) {
		wailsruntime.EventsEmit(a.ctx, "print:watch",
			jobWatchEvent{ID: id, State: state, Text: text})
	}
	deadline := time.Now().Add(20 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		jobs, err := queueSnapshot(name)
		if err != nil {
			emit("error", fmt.Sprintf("Слежение за заданием №%d: %v", id, err))
			return
		}
		var job *jobSummary
		for i := range jobs {
			if jobs[i].ID == id {
				job = &jobs[i]
				break
			}
		}
		if job == nil {
			// Задание исчезло из очереди — Windows передала данные в порт принтера.
			emit("done", fmt.Sprintf("Задание №%d передано принтеру", id))
			return
		}
		state := job.Status
		if state == "" {
			state = "ожидает отправки"
		}
		if job.StatusCode&(jobStError|jobStOffline|jobStPaperOut|jobStBlocked|jobStIntervention) != 0 {
			emit("error", fmt.Sprintf("Задание №%d: %s", id, state))
			return
		}
		if state != last {
			emit("progress", fmt.Sprintf("Задание №%d: %s (позиция %d)", id, state, job.Position))
			last = state
		}
	}
	emit("stuck", fmt.Sprintf(
		"Задание №%d висит в очереди больше 20 секунд — данные не доходят до порта принтера. Откройте диагностику.", id))
}

// openPrintQueue открывает окно очереди печати Windows для принтера.
func openPrintQueue(name string) error {
	return exec.Command("rundll32", "printui.dll,PrintUIEntry", "/o", "/n", name).Start()
}
