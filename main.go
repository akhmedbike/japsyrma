package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// buildMenu собирает нативное меню (показывается на macOS; на Windows работают
// те же действия через клавиатуру). Пункты шлют события во фронтенд.
func buildMenu(app *App) *menu.Menu {
	emit := func(name string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) { runtime.EventsEmit(app.ctx, name) }
	}

	m := menu.NewMenu()

	appMenu := m.AddSubmenu("japsyrma")
	appMenu.AddText("О программе japsyrma", nil, emit("menu:about"))
	appMenu.AddSeparator()
	appMenu.AddText("Завершить japsyrma", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		runtime.Quit(app.ctx)
	})

	file := m.AddSubmenu("Файл")
	file.AddText("Новая", keys.CmdOrCtrl("n"), emit("menu:new"))
	file.AddText("Открыть…", keys.CmdOrCtrl("o"), emit("menu:open"))
	file.AddText("Открыть последние…", nil, emit("menu:recent"))
	file.AddSeparator()
	file.AddText("Сохранить", keys.CmdOrCtrl("s"), emit("menu:save"))
	file.AddText("Сохранить как…", keys.Combo("s", keys.ShiftKey, keys.CmdOrCtrlKey), emit("menu:saveAs"))

	edit := m.AddSubmenu("Правка")
	edit.AddText("Отменить", keys.CmdOrCtrl("z"), emit("menu:undo"))
	edit.AddText("Повторить", keys.Combo("z", keys.ShiftKey, keys.CmdOrCtrlKey), emit("menu:redo"))
	edit.AddSeparator()
	edit.AddText("Копировать", keys.CmdOrCtrl("c"), emit("menu:copy"))
	edit.AddText("Вставить", keys.CmdOrCtrl("v"), emit("menu:paste"))
	edit.AddText("Дублировать", keys.CmdOrCtrl("d"), emit("menu:duplicate"))

	win := m.AddSubmenu("Окно")
	win.AddText("Свернуть", keys.CmdOrCtrl("m"), func(*menu.CallbackData) {
		runtime.WindowMinimise(app.ctx)
	})
	win.AddText("Во весь экран", nil, func(*menu.CallbackData) {
		runtime.WindowToggleMaximise(app.ctx)
	})

	return m
}

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "Этикетки",
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 201, G: 206, B: 212, A: 255},
		OnStartup:        app.startup,
		Menu:             buildMenu(app),
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
