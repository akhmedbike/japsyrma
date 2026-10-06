import { SYM as BARCODE_DEFAULTS } from './barcode.js'

// Общее состояние документа. Все координаты элементов — в точках принтера
// (203 dpi = 8 точек на мм), размеры этикетки — в мм.

export const DPI = 203
export const mmToDots = (mm) => Math.round(((Number(mm) || 0) * DPI) / 25.4)
export const dotsToMm = (d) => Math.round(((d * 25.4) / DPI) * 10) / 10
export const ptToDots = (pt) => Math.round(((Number(pt) || 0) * DPI) / 72)

export const PRESETS = [
  [58, 40], [58, 30], [40, 30], [30, 20], [43, 25],
  [75, 50], [100, 50], [100, 100], [100, 150],
]

export const FONTS = [
  'Arial', 'Arial Narrow', 'Segoe UI', 'Calibri', 'Verdana', 'Tahoma',
  'Times New Roman', 'Georgia', 'Courier New', 'Impact',
]

function defaultDoc() {
  return {
    version: 1,
    label: { widthMM: 58, heightMM: 40, gapMM: 2, media: 'gap' },
    elements: [],
  }
}

export const doc = $state(defaultDoc())

export const ui = $state({
  selectedId: null,
  zoom: 2,
  autoFit: true,
  preview: false,
  filePath: '',
  dirty: false,
})

function loadPrintCfg() {
  try {
    return JSON.parse(localStorage.getItem('printCfg') || '{}')
  } catch {
    return {}
  }
}

export const printCfg = $state({
  printer: '',
  density: 8,
  speed: 4,
  direction: 1,
  copies: 1,
  invert: false,
  ...loadPrintCfg(),
})

let seq = 0
const newId = () => `el${Date.now().toString(36)}${(seq++).toString(36)}`

export function labelDots() {
  return {
    W: Math.max(8, mmToDots(doc.label.widthMM)),
    H: Math.max(8, mmToDots(doc.label.heightMM)),
  }
}

export function selected() {
  return doc.elements.find((e) => e.id === ui.selectedId) ?? null
}

function push(el) {
  // новый массив вместо splice/push: изменение порядка и состава
  // элементов должно доходить до $effect в App.svelte
  doc.elements = [...doc.elements, el]
  ui.selectedId = el.id
  ui.dirty = true
}

// ---------- отмена / повтор ----------

// Снимки делает $effect в App.svelte (там гарантированно живой реактивный
// контекст); здесь — хранилище и сами undo/redo. Флаг suppressed гасит
// коммит сразу после undo/redo, иначе восстановление попало бы в историю.
let undoStack = []
let redoStack = []
let suppressed = false

export function pushUndo(snapshot) {
  undoStack.push(snapshot)
  if (undoStack.length > 100) undoStack.shift()
  redoStack = []
}

// effect истории в App.svelte вызывает это после undo/redo:
// восстановленное состояние не должно попасть в историю
export function consumeHistorySuppress() {
  const v = suppressed
  suppressed = false
  return v
}

function applySnapshot(s) {
  doc.label = { ...s.label }
  doc.elements = s.elements.map((e) => ({ ...e }))
  if (!doc.elements.some((e) => e.id === ui.selectedId)) ui.selectedId = null
  ui.dirty = true
}

export function undo() {
  if (!undoStack.length) return false
  const target = undoStack.pop()
  redoStack.push($state.snapshot(doc))
  suppressed = true
  applySnapshot(target)
  return true
}

export function redo() {
  if (!redoStack.length) return false
  const target = redoStack.pop()
  undoStack.push($state.snapshot(doc))
  suppressed = true
  applySnapshot(target)
  return true
}

// ---------- буфер обмена элементов ----------

let clipboard = null
let pasteStep = 0

export function copySelected() {
  const el = selected()
  if (!el) return false
  clipboard = $state.snapshot(el)
  pasteStep = 0
  return true
}

export function cutSelected() {
  if (!copySelected()) return false
  removeSelected()
  return true
}

export function pasteClipboard() {
  if (!clipboard) return false
  const off = mmToDots(2) * ++pasteStep
  push({ ...structuredClone(clipboard), id: newId(), x: clipboard.x + off, y: clipboard.y + off })
  return true
}

export function addText() {
  const { W } = labelDots()
  const m = mmToDots(2)
  push({
    id: newId(), type: 'text',
    x: m, y: m, rotation: 0,
    width: Math.max(40, W - 2 * m),
    text: 'Текст', fontFamily: 'Arial', fontSize: 14,
    bold: false, italic: false, align: 'left', lineHeight: 1.1,
  })
}

// Вставляет изображение в обведенную область, вписывая его с сохранением пропорций.
export function addImageAt(src, natW, natH, box) {
  const s = Math.min(box.w / natW, box.h / natH)
  const w = Math.max(1, Math.round(natW * s))
  const h = Math.max(1, Math.round(natH * s))
  push({
    id: newId(), type: 'image',
    x: Math.round(box.x + (box.w - w) / 2),
    y: Math.round(box.y + (box.h - h) / 2),
    rotation: 0, width: w, height: h, src,
    mode: 'dither', threshold: 128, brightness: 0, contrast: 1, invert: false,
  })
}

export function addRect(line = false) {
  const { W, H } = labelDots()
  const m = mmToDots(2)
  push({
    id: newId(), type: 'rect',
    x: m, y: line ? Math.round(H / 2) : m, rotation: 0,
    width: W - 2 * m, height: line ? 3 : H - 2 * m,
    fill: line, strokeWidth: 3,
  })
}

// Эллипс и треугольник: по умолчанию 20 мм, вписанные в этикетку.
export function addShape(kind /* 'ellipse' | 'triangle' */) {
  const { W, H } = labelDots()
  const m = mmToDots(2)
  const w = Math.min(mmToDots(20), W - 2 * m)
  const h = Math.min(mmToDots(20), H - 2 * m)
  push({
    id: newId(), type: 'shape', kind,
    x: Math.round((W - w) / 2), y: Math.round((H - h) / 2), rotation: 0,
    width: w, height: h, fill: false, strokeWidth: 3,
  })
}

export function addBarcode(symbology = 'code128') {
  const sym = BARCODE_DEFAULTS[symbology] ?? BARCODE_DEFAULTS.code128
  const m = mmToDots(2)
  push({
    id: newId(), type: 'barcode',
    x: m, y: m, rotation: 0,
    symbology, value: sym.sample, module: sym.module,
    barHeight: mmToDots(10), showText: true, textSize: 8,
  })
}

export function updateElement(id, patch) {
  const el = doc.elements.find((e) => e.id === id)
  if (!el) return
  Object.assign(el, patch)
  ui.dirty = true
}

export function removeSelected() {
  const i = doc.elements.findIndex((e) => e.id === ui.selectedId)
  if (i < 0) return
  doc.elements = doc.elements.filter((_, idx) => idx !== i)
  ui.selectedId = null
  ui.dirty = true
}

export function duplicateSelected() {
  const el = selected()
  if (!el) return
  const d = mmToDots(1)
  push({ ...$state.snapshot(el), id: newId(), x: el.x + d, y: el.y + d })
}

// dir = +1 — выше (ближе к зрителю), -1 — ниже. false — двигать некуда.
export function moveSelected(dir) {
  const i = doc.elements.findIndex((e) => e.id === ui.selectedId)
  const j = i + dir
  if (i < 0 || j < 0 || j >= doc.elements.length) return false
  const arr = [...doc.elements]
  arr.splice(j, 0, arr.splice(i, 1)[0])
  doc.elements = arr
  ui.dirty = true
  return true
}

export function newDoc() {
  Object.assign(doc, defaultDoc())
  ui.selectedId = null
  ui.filePath = ''
  ui.dirty = false
}

export function serialize() {
  return $state.snapshot(doc)
}

export function loadDoc(data) {
  if (!data || typeof data !== 'object' || !data.label || !Array.isArray(data.elements)) {
    throw new Error('Файл не похож на шаблон этикетки')
  }
  const d = defaultDoc()
  doc.version = data.version ?? 1
  doc.label = { ...d.label, ...data.label }
  doc.elements = data.elements
  ui.selectedId = null
  ui.dirty = false
}
