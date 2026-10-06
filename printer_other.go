//go:build !windows

package main

import (
	"errors"
	"os"
	"strings"
)

func listPrinters() ([]string, string, error) {
	return nil, "", errors.New("список принтеров есть только в Windows; на Linux используйте -printer /dev/usb/lp0")
}

func sendRaw(name string, data []byte) (uint32, error) {
	if !strings.HasPrefix(name, "/dev/") {
		return 0, errors.New("на этой ОС укажите -printer /dev/usb/lpN")
	}
	f, err := os.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	_, err = f.Write(data)
	return 0, err
}

func probeRaw(name string) error {
	if !strings.HasPrefix(name, "/dev/") {
		return errors.New("на этой ОС печать только в /dev/usb/lpN")
	}
	f, err := os.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

func readSpooler(name string) (*SpoolerState, error) {
	return nil, errors.New("спулер доступен только в Windows")
}

func queueSnapshot(name string) ([]jobSummary, error) {
	return nil, errors.New("очередь печати доступна только в Windows")
}

func cancelAllJobs(name string) (int, error) {
	return 0, errors.New("очередь печати доступна только в Windows")
}

func (a *App) watchJob(name string, id uint32) {}

func openPrintQueue(name string) error {
	return errors.New("очередь печати доступна только в Windows")
}
