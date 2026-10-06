<script>
  import { onMount, untrack } from 'svelte'
  import { Editor } from './lib/editor.js'
  import { loadImage } from './lib/mono.js'
  import Inspector from './lib/Inspector.svelte'
  import { renderBarcode } from './lib/barcode.js'
  import {
    doc, ui, printCfg, PRESETS, DPI, mmToDots, labelDots, selected,
    addText, addImageAt, addRect, addShape, addBarcode, updateElement, removeSelected, duplicateSelected,
    newDoc, serialize, loadDoc, undo, redo, copySelected, cutSelected, pasteClipboard,
    pushUndo, consumeHistorySuppress,
  } from './lib/state.svelte.js'
  import { showToast, toast } from './lib/toast.svelte.js'
  import {
    ListPrinters, Print, FeedLabel, ExportPRN, ExportFile, ProofPDF, SaveTemplate, OpenTemplate,
    OpenImage, LoadTemplate, Diagnose, OpenPrintQueue, CancelAllJobs,
  } from '../wailsjs/go/main/App.js'
  import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime.js'

  let host
  let desk
  let editor = $state.raw(null)
  let printers = $state([])
  let printersError = $state('')
  let busy = $state(false)
  let status = $state({ text: 'Готово', kind: 'info' })
  let monoURL = $state('')
  let aboutOpen = $state(false)
  let recentOpen = $state(false)
  let recent = $state([])
  let diagOpen = $state(false)
  let diag = $state([])
  let diagBusy = $state(false)

  const dims = $derived(labelDots())
  const W = $derived(dims.W)
  const H = $derived(dims.H)
  const sel = $derived(doc.elements.find((e) => e.id === ui.selectedId) ?? null)
  const realZoom = 96 / DPI // при этом масштабе этикетка близка к натуральному размеру
  const tooWide = $derived(doc.label.widthMM > 108)
  const badBarcodes = $derived(
    doc.elements.filter((e) => e.type === 'barcode' && renderBarcode(e).error).length,
  )
  const cannotPrint = $derived(tooWide || badBarcodes > 0)

  function say(text, kind = 'info') {
    status = { text, kind }
  }

  onMount(() => {
    editor = new Editor(host, {
      onSelect: (id) => (ui.selectedId = id),
      onChange: (id, patch) => updateElement(id, patch),
    })
    loadPrinters()
    loadRecent()
    // нативное меню (macOS) шлет события; на Windows работают клавиши напрямую
    EventsOn('menu:new', () => onNew())
    EventsOn('menu:open', () => onOpen())
    EventsOn('menu:save', () => onSave(false))
    EventsOn('menu:saveAs', () => onSave(true))
    EventsOn('menu:recent', () => (recentOpen = true))
    EventsOn('menu:about', () => (aboutOpen = true))
    EventsOn('menu:undo', () => undoAction())
    EventsOn('menu:redo', () => redoAction())
    EventsOn('menu:duplicate', () => duplicateSelected())
    EventsOn('menu:copy', () => copyAction())
    EventsOn('menu:paste', () => pasteAction())
    // Слежение за заданием в очереди после отправки на печать.
    EventsOn('print:watch', (p) => {
      status.text = p.text
      status.kind = p.state === 'error' || p.state === 'stuck' ? 'error' : 'ok'
      if (p.state === 'done') showToast(p.text)
      else if (p.state === 'error' || p.state === 'stuck') showToast(p.text, 'error')
    })
    const ro = new ResizeObserver(() => ui.autoFit && fit())
    ro.observe(desk)
    return () => {
      ro.disconnect()
      editor.destroy()
    }
  })

  // путь открытого файла — в заголовке окна
  $effect(() => {
    const title = ui.filePath ? `Этикетки — ${ui.filePath}` : 'Этикетки'
    try {
      WindowSetTitle(title)
    } catch {
      /* в обычном браузере runtime недоступен */
    }
  })

  // история отмены: снимок doc до каждого изменения; правки моложе 400 мс
  // склеиваются, чтобы Ctrl+Z отменял действие целиком
  let prevDoc = $state.snapshot(doc)
  let lastPushAt = 0
  $effect(() => {
    $state.snapshot(doc)
    untrack(() => {
      if (consumeHistorySuppress()) {
        prevDoc = $state.snapshot(doc)
        return
      }
      const now = Date.now()
      if (now - lastPushAt >= 400) {
        pushUndo(prevDoc)
        lastPushAt = now
      }
      prevDoc = $state.snapshot(doc)
    })
  })

  $effect(() => {
    editor?.setLabel(W, H)
  })

  $effect(() => {
    W, H
    if (ui.autoFit && desk) untrack(fit)
  })

  $effect(() => {
    editor?.setZoom(ui.zoom)
  })

  $effect(() => {
    const els = $state.snapshot(doc.elements)
    const id = ui.selectedId
    editor?.sync(els, id)
  })

  // превью «как напечатается»
  $effect(() => {
    if (!ui.preview || !editor) return
    $state.snapshot(doc)
    W, H
    let cancelled = false
    editor.ready().then(() => {
      if (!cancelled) monoURL = editor.renderMono().toDataURL('image/png')
    })
    return () => (cancelled = true)
  })

  $effect(() => {
    localStorage.setItem('printCfg', JSON.stringify($state.snapshot(printCfg)))
  })

  function fit() {
    if (!desk) return
    const z = Math.min((desk.clientWidth - 140) / W, (desk.clientHeight - 160) / H)
    ui.zoom = Math.min(12, Math.max(0.25, z))
    ui.autoFit = true
  }

  function zoomBy(f) {
    ui.zoom = Math.min(12, Math.max(0.25, ui.zoom * f))
    ui.autoFit = false
  }

  async function loadPrinters() {
    try {
      const r = await ListPrinters()
      printers = r.names ?? []
      printersError = ''
      if (!printCfg.printer || !printers.includes(printCfg.printer)) {
        printCfg.printer = printers.find((n) => /TSC|TE200/i.test(n)) ?? r.default ?? ''
      }
    } catch (e) {
      printersError = String(e)
    }
  }

  function confirmDiscard() {
    return !ui.dirty || confirm('Несохраненные изменения будут потеряны. Продолжить?')
  }

  // ---------- недавние файлы ----------

  function loadRecent() {
    try {
      recent = JSON.parse(localStorage.getItem('recentFiles')) || []
    } catch {
      recent = []
    }
  }

  function pushRecent(path) {
    if (!path) return
    recent = [path, ...recent.filter((p) => p !== path)].slice(0, 10)
    localStorage.setItem('recentFiles', JSON.stringify(recent))
  }

  async function openRecent(path) {
    if (!confirmDiscard()) return
    try {
      const r = await LoadTemplate(path)
      loadDoc(JSON.parse(r.content))
      ui.filePath = r.path
      pushRecent(r.path)
      recentOpen = false
      say('Шаблон открыт')
      showToast(`Открыто: ${r.path}`)
    } catch (e) {
      showToast(`Не удалось открыть: ${e}`, 'error')
    }
  }

  // ---------- буфер обмена и отмена ----------

  function editingText() {
    const t = document.activeElement
    return t instanceof HTMLElement && (t.closest('input, textarea, select') || t.isContentEditable)
  }

  function copyAction() {
    if (editingText()) {
      document.execCommand('copy') // поле ввода — обычное копирование текста
      return
    }
    if (copySelected()) showToast('Элемент скопирован')
  }

  function pasteAction() {
    if (editingText()) {
      document.execCommand('paste')
      return
    }
    if (!pasteClipboard()) showToast('Буфер обмена пуст', 'info')
  }

  function undoAction() {
    if (undo()) showToast('Отменено')
  }

  function redoAction() {
    if (redo()) showToast('Повторено')
  }

  // ---------- диагностика печати ----------

  async function runDiag() {
    diagBusy = true
    try {
      diag = await Diagnose(printCfg.printer || '')
      diagOpen = true
    } catch (e) {
      showToast(`Диагностика не удалась: ${e}`, 'error')
    } finally {
      diagBusy = false
    }
  }

  async function openQueue() {
    try {
      await OpenPrintQueue(printCfg.printer)
    } catch (e) {
      showToast(`Очередь не открылась: ${e}`, 'error')
    }
  }

  async function cancelJobs() {
    try {
      const n = await CancelAllJobs(printCfg.printer || '')
      showToast(n > 0 ? `Удалено заданий: ${n}` : 'Очередь и так пуста')
      runDiag()
    } catch (e) {
      showToast(`Не удалось очистить очередь: ${e}`, 'error')
    }
  }

  function onNew() {
    stopPlacing()
    if (confirmDiscard()) newDoc()
  }

  async function onOpen() {
    stopPlacing()
    if (!confirmDiscard()) return
    try {
      const r = await OpenTemplate()
      if (!r) return
      loadDoc(JSON.parse(r.content))
      ui.filePath = r.path
      pushRecent(r.path)
      say('Шаблон открыт')
      showToast(`Открыто: ${r.path}`)
    } catch (e) {
      say(`Не удалось открыть шаблон: ${e}`, 'error')
    }
  }

  async function onSave(saveAs = false) {
    try {
      const p = await SaveTemplate(JSON.stringify(serialize()), saveAs ? '' : ui.filePath)
      if (!p) return
      ui.filePath = p
      ui.dirty = false
      pushRecent(p)
      say('Шаблон сохранен')
      showToast(`Сохранено: ${p}`)
    } catch (e) {
      say(`Не удалось сохранить: ${e}`, 'error')
      showToast(`Не удалось сохранить: ${e}`, 'error')
    }
  }

  // Добавление изображения: нативный диалог выбора файла, затем пользователь
  // обводит область на этикетке, куда вписать картинку. Esc или правый клик — отмена.
  async function onImage() {
    stopPlacing()
    try {
      const src = await OpenImage()
      if (!src) return
      const img = await loadImage(src)
      const natW = img.naturalWidth || 200
      const natH = img.naturalHeight || 200
      say('Обведите область на этикетке, куда вставить изображение (Esc — отмена)')
      editor.startPlacement((box) => {
        addImageAt(src, natW, natH, box)
        say('Изображение добавлено', 'ok')
      })
    } catch (e) {
      say(`Не удалось добавить изображение: ${e.message ?? e}`, 'error')
    }
  }

  function stopPlacing() {
    if (editor?.placing) {
      editor.cancelPlacement()
      say('Вставка изображения отменена')
    }
  }

  async function request(withImage) {
    const req = {
      printer: printCfg.printer,
      widthMM: Number(doc.label.widthMM),
      heightMM: Number(doc.label.heightMM),
      gapMM: Number(doc.label.gapMM) || 0,
      media: doc.label.media,
      density: Number(printCfg.density),
      speed: Number(printCfg.speed),
      direction: Number(printCfg.direction),
      copies: Math.max(1, Number(printCfg.copies) || 1),
      invert: printCfg.invert,
      pngBase64: '',
    }
    if (withImage) {
      await editor.ready()
      req.pngBase64 = editor.renderMono().toDataURL('image/png').split(',')[1]
    }
    return req
  }

  async function run(label, fn) {
    if (busy) return
    busy = true
    say(label)
    try {
      await fn()
    } catch (e) {
      say(`Ошибка: ${e}`, 'error')
      showToast(`Ошибка: ${e}`, 'error')
    } finally {
      busy = false
    }
  }

  const doPrint = () =>
    cannotPrint ? showToast(badBarcodes ? 'Исправьте значения штрихкодов перед печатью' : 'Ширина этикетки больше 108 мм', 'error') :
    run('Отправка на принтер…', async () => {
      const msg = await Print(await request(true))
      say(msg, 'ok')
      showToast(msg)
    })

  const doFeed = () =>
    run('Прогон этикетки…', async () => {
      await FeedLabel(await request(false))
      say('Этикетка прогнана', 'ok')
    })

  const doExport = () =>
    run('Подготовка задания…', async () => {
      const p = await ExportPRN(await request(true))
      say(p ? `Задание сохранено: ${p}` : 'Готово')
    })

  // экспорт и пробный оттиск: ширина не ограничена, но штрихкоды должны быть верными
  const exportBlocked = () => {
    if (badBarcodes) {
      say('Исправьте значения штрихкодов перед экспортом', 'error')
      return true
    }
    return false
  }

  const doExportFile = (format) =>
    exportBlocked() ||
    run('Подготовка файла…', async () => {
      const p = await ExportFile(await request(true), format)
      say(p ? `Сохранено: ${p}` : 'Готово', p ? 'ok' : 'info')
      if (p) showToast(`Сохранено: ${p}`)
    })

  const doProof = () =>
    exportBlocked() ||
    run('Подготовка оттиска…', async () => {
      await ProofPDF(await request(true))
      say('PDF открыт. При печати выберите «Фактический размер», без масштабирования', 'ok')
    })

  function onKey(e) {
    const t = e.target
    const typing = t instanceof HTMLElement && (t.closest('input, textarea, select') || t.isContentEditable)
    if (e.ctrlKey || e.metaKey) {
      // e.code не зависит от раскладки
      if (e.code === 'KeyS') { e.preventDefault(); onSave(e.shiftKey) }
      else if (e.code === 'KeyO') { e.preventDefault(); onOpen() }
      else if (e.code === 'KeyP') { e.preventDefault(); doPrint() }
      else if (e.code === 'KeyD' && !typing) { e.preventDefault(); duplicateSelected() }
      else if (e.code === 'KeyZ' && !typing) { e.preventDefault(); e.shiftKey ? redoAction() : undoAction() }
      else if (e.code === 'KeyC' && !typing) { e.preventDefault(); copyAction() }
      else if (e.code === 'KeyV' && !typing) { e.preventDefault(); pasteAction() }
      else if (e.code === 'KeyX' && !typing) { e.preventDefault(); cutSelected() }
      return
    }
    if (typing) return
    const el = selected()
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (el) { e.preventDefault(); removeSelected() }
    } else if (e.key === 'Escape') {
      if (diagOpen) diagOpen = false
      else if (recentOpen) recentOpen = false
      else if (aboutOpen) aboutOpen = false
      else if (editor?.placing) stopPlacing()
      else ui.selectedId = null
    } else if (el && e.key.startsWith('Arrow')) {
      e.preventDefault()
      const s = e.shiftKey ? mmToDots(1) : 1
      el.x += e.key === 'ArrowLeft' ? -s : e.key === 'ArrowRight' ? s : 0
      el.y += e.key === 'ArrowUp' ? -s : e.key === 'ArrowDown' ? s : 0
      ui.dirty = true
    }
  }

  const fileName = $derived(ui.filePath ? ui.filePath.split(/[\\/]/).pop() : 'Без названия')
  const pxW = $derived(Math.ceil(W * ui.zoom))
  const pxH = $derived(Math.ceil(H * ui.zoom))
  const gapPx = $derived(Math.max(6, mmToDots(doc.label.gapMM) * ui.zoom))
  const radius = $derived(Math.min(mmToDots(1.5) * ui.zoom, 14))
</script>

<svelte:window onkeydown={onKey} />

<div class="app">
  <header class="toolbar">
    <div class="group">
      <button onclick={onNew}>Новая</button>
      <button onclick={onOpen} title="Ctrl+O">Открыть</button>
      <button onclick={() => (recentOpen = true)} title="Последние шаблоны">Недавние</button>
      <button onclick={() => onSave(false)} title="Ctrl+S">Сохранить</button>
      <button onclick={() => onSave(true)} title="Ctrl+Shift+S">Сохранить как</button>
    </div>
    <div class="group">
      <button onclick={() => { stopPlacing(); addText() }}>Текст</button>
      <button onclick={onImage}>Изображение</button>
      <button onclick={() => { stopPlacing(); addBarcode('code128') }}>Штрихкод</button>
      <button onclick={() => { stopPlacing(); addBarcode('qrcode') }}>QR</button>
      <button onclick={() => { stopPlacing(); addRect(false) }}>Рамка</button>
      <button onclick={() => { stopPlacing(); addRect(true) }}>Линия</button>
      <button onclick={() => { stopPlacing(); addShape('ellipse') }}>Эллипс</button>
      <button onclick={() => { stopPlacing(); addShape('triangle') }}>Треугольник</button>
    </div>
    <div class="group end">
      <button class="icon" onclick={() => zoomBy(1 / 1.25)} aria-label="Уменьшить">−</button>
      <button class="zoom" onclick={() => { ui.zoom = realZoom; ui.autoFit = false }} title="Натуральный размер">
        {Math.round((ui.zoom / realZoom) * 100)}%
      </button>
      <button class="icon" onclick={() => zoomBy(1.25)} aria-label="Увеличить">+</button>
      <button onclick={fit}>Вписать</button>
      <label class="switch">
        <input type="checkbox" bind:checked={ui.preview} />
        <span>Как напечатается</span>
      </label>
      <button class="primary" onclick={doPrint} disabled={busy || cannotPrint} title="Ctrl+P">Печать</button>
      <button class="icon" onclick={() => (aboutOpen = true)} title="О программе" aria-label="О программе">?</button>
    </div>
  </header>

  <main class="desk" bind:this={desk}>
    <div class="liner" class:continuous={doc.label.media === 'cont'} style:width="{pxW + 48}px">
      {#if doc.label.media !== 'cont'}
        <div class="ghost top" style:border-radius="0 0 {radius}px {radius}px"></div>
        <div class="gap" style:height="{gapPx}px">
          {#if doc.label.media === 'bline'}<span class="mark"></span>{/if}
        </div>
      {/if}
      <div class="label" style:width="{pxW}px" style:height="{pxH}px" style:border-radius="{radius}px">
        <div bind:this={host} class="stage"></div>
        {#if ui.preview && monoURL}
          <img class="mono" src={monoURL} alt="" style:width="{pxW}px" style:height="{pxH}px" />
        {/if}
      </div>
      {#if doc.label.media !== 'cont'}
        <div class="gap" style:height="{gapPx}px">
          {#if doc.label.media === 'bline'}<span class="mark"></span>{/if}
        </div>
        <div class="ghost bottom" style:border-radius="{radius}px {radius}px 0 0"></div>
      {/if}
    </div>
    {#if doc.elements.length === 0}
      <p class="empty">Добавьте текст или изображение кнопками сверху</p>
    {/if}
  </main>

  <aside class="side">
    <section class="panel">
      <h3>Этикетка</h3>
      <div class="grid2">
        <label class="field">
          <span>Ширина, мм</span>
          <input type="number" min="5" max="108" step="0.5" bind:value={doc.label.widthMM} class:invalid={tooWide} />
        </label>
        <label class="field">
          <span>Высота, мм</span>
          <input type="number" min="5" max="1000" step="0.5" bind:value={doc.label.heightMM} />
        </label>
        <label class="field">
          <span>Материал</span>
          <select bind:value={doc.label.media}>
            <option value="gap">С зазором</option>
            <option value="bline">С черной меткой</option>
            <option value="cont">Непрерывная лента</option>
          </select>
        </label>
        {#if doc.label.media !== 'cont'}
          <label class="field">
            <span>{doc.label.media === 'bline' ? 'Метка, мм' : 'Зазор, мм'}</span>
            <input type="number" min="0" max="20" step="0.5" bind:value={doc.label.gapMM} />
          </label>
        {/if}
      </div>
      {#if tooWide}<p class="hint error">Ширина печати TE200 — не больше 108 мм.</p>{/if}
      <div class="presets">
        {#each PRESETS as [w, h]}
          <button
            class="chip"
            class:on={doc.label.widthMM === w && doc.label.heightMM === h}
            onclick={() => { doc.label.widthMM = w; doc.label.heightMM = h; ui.dirty = true }}
          >{w}×{h}</button>
        {/each}
      </div>
    </section>

    {#if sel}
      {#key sel.id}
        <Inspector el={sel} />
      {/key}
    {/if}

    <section class="panel">
      <h3>Печать</h3>
      <label class="field wide">
        <span>Принтер</span>
        <div class="row tight">
          <select bind:value={printCfg.printer}>
            {#if !printers.length}<option value="">Принтеры не найдены</option>{/if}
            {#each printers as p}<option value={p}>{p}</option>{/each}
          </select>
          <button class="icon" onclick={loadPrinters} aria-label="Обновить список" title="Обновить список">↻</button>
        </div>
      </label>
      {#if printersError}<p class="hint error">{printersError}</p>{/if}
      {#if badBarcodes}<p class="hint error">Штрихкодов с ошибкой: {badBarcodes}. Печать недоступна.</p>{/if}
      <div class="grid2">
        <label class="field">
          <span>Копии</span>
          <input type="number" min="1" max="9999" bind:value={printCfg.copies} />
        </label>
        <label class="field">
          <span>Скорость, дюйм/с</span>
          <select bind:value={printCfg.speed}>
            {#each [2, 3, 4, 5, 6] as s}<option value={s}>{s}</option>{/each}
          </select>
        </label>
        <label class="field wide">
          <span>Плотность <output>{printCfg.density}</output></span>
          <input type="range" min="0" max="15" bind:value={printCfg.density} />
        </label>
        <label class="field">
          <span>Направление</span>
          <select bind:value={printCfg.direction}>
            <option value={1}>Обычное</option>
            <option value={0}>Перевернуть</option>
          </select>
        </label>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={printCfg.invert} /> Инвертировать растр
      </label>
      <p class="hint">Включите, если печать вышла негативом.</p>
      <div class="row">
        <button onclick={doFeed} disabled={busy}>Прогнать этикетку</button>
        <button onclick={doExport} disabled={busy || cannotPrint}>Сохранить .prn</button>
      </div>
      <div class="row">
        <button onclick={runDiag} disabled={diagBusy}>Диагностика</button>
      </div>
      {#if diagBusy}<p class="hint">Опрашиваю принтер и спулер…</p>{/if}
    </section>

    <section class="panel">
      <h3>Экспорт</h3>
      <div class="row">
        <button onclick={() => doExportFile('pdf')} disabled={busy || badBarcodes > 0}>PDF</button>
        <button onclick={() => doExportFile('png')} disabled={busy || badBarcodes > 0}>PNG</button>
        <button onclick={doProof} disabled={busy || badBarcodes > 0}>Пробный оттиск</button>
      </div>
      <p class="hint">
        Натуральный размер, ровно то, что уйдет на термопринтер. Пробный оттиск открывает PDF
        для печати на обычном принтере — проверить размеры и читаемость кодов.
      </p>
    </section>
  </aside>

  <footer class="status">
    <span class="file">{fileName}{ui.dirty ? ' *' : ''}</span>
    <span>{doc.label.widthMM} × {doc.label.heightMM} мм</span>
    <span>{W} × {H} точек</span>
    <span class="msg {status.kind}">{status.text}</span>
  </footer>

  {#if toast.visible}
    <div class="toast {toast.kind}" role="status">{toast.text}</div>
  {/if}

  {#if aboutOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="overlay" onclick={() => (aboutOpen = false)}>
      <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events, a11y_interactive_supports_focus -->
      <div class="modal" role="dialog" aria-label="О программе" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h2>О программе</h2>
        <p><b>japsyrma</b> — редактор этикеток для термопринтеров TSC.</p>
        <p>Печать — RAW TSPL через спулер Windows. Wails (Go) + Svelte + Konva. Версия 0.1.0.</p>
        <div class="row end">
          <button onclick={() => (aboutOpen = false)}>Закрыть</button>
        </div>
      </div>
    </div>
  {/if}

  {#if diagOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="overlay" onclick={() => (diagOpen = false)}>
      <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events, a11y_interactive_supports_focus -->
      <div class="modal wide" role="dialog" aria-label="Диагностика печати" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h2>Диагностика печати</h2>
        <ul class="diag">
          {#each diag as d (d.name)}
            <li>
              <span class="dot {d.status}" title={d.status}></span>
              <div>
                <b>{d.name}</b>
                <span>{d.detail}</span>
              </div>
            </li>
          {/each}
        </ul>
        <div class="row end">
          <button onclick={cancelJobs}>Отменить все задания</button>
          <button onclick={openQueue}>Открыть очередь Windows</button>
          <button onclick={() => (diagOpen = false)}>Закрыть</button>
        </div>
      </div>
    </div>
  {/if}

  {#if recentOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="overlay" onclick={() => (recentOpen = false)}>
      <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events, a11y_interactive_supports_focus -->
      <div class="modal" role="dialog" aria-label="Открыть последние" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h2>Открыть последние</h2>
        {#if recent.length === 0}
          <p class="hint">Пока нет недавних файлов.</p>
        {:else}
          <ul class="recent">
            {#each recent as p (p)}
              <li>
                <button class="recent-item" onclick={() => openRecent(p)}>
                  <b>{p.split(/[\\/]/).pop()}</b>
                  <span>{p}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        <div class="row end">
          <button onclick={() => (recentOpen = false)}>Отмена</button>
        </div>
      </div>
    </div>
  {/if}
</div>
