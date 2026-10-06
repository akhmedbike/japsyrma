// Обертка над Konva. 1 единица сцены = 1 точка принтера; масштаб экрана — zoom.
import Konva from 'konva'
import { loadImage, processImage, canvasToMono } from './mono.js'
import { ptToDots } from './state.svelte.js'
import { renderBarcode } from './barcode.js'

const ALL = ['top-left', 'top-center', 'top-right', 'middle-left', 'middle-right', 'bottom-left', 'bottom-center', 'bottom-right']
// размер штрихкода задается модулем, а не растягиванием
const ANCHORS = { text: ['middle-left', 'middle-right'], image: ALL, rect: ALL, shape: ALL, barcode: [] }

// Фигуры рисуем сами внутри прямоугольника width×height из точки (0,0).
const SHAPE_SCENE = {
  ellipse: (ctx, shape) => {
    const w = shape.width()
    const h = shape.height()
    ctx.beginPath()
    ctx.ellipse(w / 2, h / 2, w / 2, h / 2, 0, 0, Math.PI * 2)
    ctx.closePath()
    ctx.fillStrokeShape(shape)
  },
  triangle: (ctx, shape) => {
    const w = shape.width()
    const h = shape.height()
    ctx.beginPath()
    ctx.moveTo(w / 2, 0)
    ctx.lineTo(w, h)
    ctx.lineTo(0, h)
    ctx.closePath()
    ctx.fillStrokeShape(shape)
  },
}

export class Editor {
  constructor(container, { onSelect, onChange }) {
    this.onSelect = onSelect
    this.onChange = onChange
    this.W = 1
    this.H = 1
    this.zoom = 1
    this.nodes = new Map()     // id -> Konva node
    this.images = new Map()    // id -> { src, params, canvas, token }
    this.originals = new Map() // src -> Promise<HTMLImageElement>
    this.pending = new Set()

    this.stage = new Konva.Stage({ container, width: 1, height: 1 })
    this.layer = new Konva.Layer({ imageSmoothingEnabled: false })
    this.stage.add(this.layer)
    this.bg = new Konva.Rect({ x: 0, y: 0, fill: '#ffffff' })
    this.content = new Konva.Group()
    this.tr = new Konva.Transformer({
      rotationSnaps: [0, 90, 180, 270],
      rotationSnapTolerance: 10,
      flipEnabled: false,
      ignoreStroke: true,
      anchorSize: 9,
      anchorCornerRadius: 2,
      borderStroke: '#1f5fbf',
      anchorStroke: '#1f5fbf',
      boundBoxFunc: (oldBox, box) => (Math.abs(box.width) < 6 || Math.abs(box.height) < 3 ? oldBox : box),
    })
    this.layer.add(this.bg, this.content, this.tr)

    this.stage.on('mousedown touchstart', (e) => {
      if (e.target === this.stage || e.target === this.bg) this.onSelect(null)
    })
  }

  setLabel(W, H) {
    this.W = W
    this.H = H
    this.bg.size({ width: W, height: H })
    this.applySize()
  }

  setZoom(z) {
    this.zoom = z
    this.applySize()
  }

  applySize() {
    this.stage.size({ width: Math.ceil(this.W * this.zoom), height: Math.ceil(this.H * this.zoom) })
    this.stage.scale({ x: this.zoom, y: this.zoom })
    this.layer.batchDraw()
  }

  // Приводит сцену в соответствие с массивом элементов.
  sync(elements, selectedId) {
    const seen = new Set()
    elements.forEach((el, idx) => {
      seen.add(el.id)
      let node = this.nodes.get(el.id)
      if (node && (node.getAttr('elType') !== el.type || node.getAttr('elKind') !== el.kind)) {
        node.destroy()
        node = null
      }
      if (!node) {
        node = this.create(el)
        this.content.add(node)
        this.nodes.set(el.id, node)
      }
      this.update(node, el)
      node.zIndex(idx)
    })
    for (const [id, node] of this.nodes) {
      if (!seen.has(id)) {
        node.destroy()
        this.nodes.delete(id)
        this.images.delete(id)
      }
    }
    const sel = selectedId ? this.nodes.get(selectedId) : null
    if (sel) {
      const type = sel.getAttr('elType')
      this.tr.enabledAnchors(ANCHORS[type])
      this.tr.keepRatio(type === 'image')
      this.tr.nodes([sel])
    } else {
      this.tr.nodes([])
    }
    this.layer.batchDraw()
  }

  create(el) {
    const id = el.id
    const cfg = { id, draggable: true, elType: el.type, elKind: el.kind }
    let node
    if (el.type === 'text') node = new Konva.Text(cfg)
    else if (el.type === 'image' || el.type === 'barcode') node = new Konva.Image(cfg)
    else if (el.type === 'shape') node = new Konva.Shape({ ...cfg, sceneFunc: SHAPE_SCENE[el.kind] })
    else node = new Konva.Rect(cfg)

    node.on('mousedown touchstart', () => this.onSelect(id))
    node.on('dragmove', () => node.position({ x: Math.round(node.x()), y: Math.round(node.y()) }))
    node.on('dragend', () => this.onChange(id, { x: Math.round(node.x()), y: Math.round(node.y()) }))
    if (el.type === 'text') {
      // текст меняет ширину блока, а не растягивается
      node.on('transform', () => {
        node.setAttrs({ width: Math.max(8, node.width() * node.scaleX()), scaleX: 1, scaleY: 1 })
      })
    }
    node.on('transformend', () => {
      const sx = node.scaleX()
      const sy = node.scaleY()
      node.scale({ x: 1, y: 1 })
      const patch = {
        x: Math.round(node.x()),
        y: Math.round(node.y()),
        rotation: Math.round(node.rotation()),
      }
      if (el.type !== 'barcode') patch.width = Math.max(1, Math.round(node.width() * sx))
      if (el.type === 'image' || el.type === 'rect' || el.type === 'shape') {
        patch.height = Math.max(1, Math.round(node.height() * sy))
      }
      this.onChange(id, patch)
    })
    return node
  }

  update(node, el) {
    node.setAttrs({ x: el.x ?? 0, y: el.y ?? 0, rotation: el.rotation || 0 })
    if (el.type === 'text') {
      const style = el.italic && el.bold ? 'italic bold' : el.italic ? 'italic' : el.bold ? 'bold' : 'normal'
      node.setAttrs({
        text: el.text ?? '',
        fontFamily: el.fontFamily || 'Arial',
        fontSize: Math.max(1, ptToDots(el.fontSize || 1)),
        fontStyle: style,
        align: el.align || 'left',
        width: Math.max(8, el.width || 8),
        lineHeight: el.lineHeight || 1.1,
        wrap: 'word',
        fill: '#000',
      })
    } else if (el.type === 'rect' || el.type === 'shape') {
      const sw = Math.max(1, el.strokeWidth || 1)
      node.setAttrs({
        width: Math.max(1, el.width),
        height: Math.max(1, el.height),
        // прозрачная заливка нужна, чтобы фигуру можно было схватить изнутри
        fill: el.fill ? '#000' : 'rgba(0,0,0,0)',
        stroke: el.fill ? null : '#000',
        strokeWidth: el.fill ? 0 : sw,
      })
    } else if (el.type === 'image') {
      node.setAttrs({ width: Math.max(1, el.width), height: Math.max(1, el.height) })
      this.updateImage(node, el)
    } else if (el.type === 'barcode') {
      const { canvas } = renderBarcode(el)
      node.setAttrs({ image: canvas, width: canvas.width, height: canvas.height })
    }
  }

  original(src) {
    if (!this.originals.has(src)) this.originals.set(src, loadImage(src))
    return this.originals.get(src)
  }

  // Перерасчет 1-битной версии изображения при смене размера или настроек.
  updateImage(node, el) {
    const params = JSON.stringify([el.width, el.height, el.mode, el.threshold, el.brightness, el.contrast, el.invert])
    const prev = this.images.get(el.id)
    if (prev && prev.src === el.src && prev.params === params) {
      if (prev.canvas && node.image() !== prev.canvas) node.image(prev.canvas)
      return
    }
    const token = Symbol()
    const entry = { src: el.src, params, canvas: prev?.canvas ?? null, token }
    this.images.set(el.id, entry)
    if (entry.canvas) node.image(entry.canvas)

    const opts = {
      mode: el.mode, threshold: el.threshold, brightness: el.brightness,
      contrast: el.contrast, invert: el.invert,
    }
    const job = this.original(el.src)
      .then((img) => {
        const cur = this.images.get(el.id)
        if (!cur || cur.token !== token) return
        cur.canvas = processImage(img, el.width, el.height, opts)
        node.image(cur.canvas)
        this.layer.batchDraw()
      })
      .catch((err) => console.error(err))
      .finally(() => this.pending.delete(job))
    this.pending.add(job)
  }

  async ready() {
    while (this.pending.size) await Promise.all([...this.pending])
  }

  // Режим обводки области: пользователь зажимает мышь и обводит прямоугольник
  // на этикетке, onDone получает {x, y, w, h} в точках принтера.
  // Esc или правый клик — отмена.
  startPlacement(onDone) {
    this.cancelPlacement()
    this.placing = true
    this.placeCb = onDone
    this.layer.listening(false) // не таскать и не выделять элементы под курсором
    this.stage.container().style.cursor = 'crosshair'

    this.placeLayer = new Konva.Layer()
    this.placeRect = new Konva.Rect({
      fill: 'rgba(31,95,191,0.10)',
      stroke: '#1f5fbf',
      strokeWidth: 2 / this.zoom,
      dash: [6 / this.zoom, 4 / this.zoom],
      visible: false,
      listening: false,
    })
    this.placeLayer.add(this.placeRect)
    this.stage.add(this.placeLayer)

    let start = null
    const h = {
      down: (e) => {
        e.evt.preventDefault()
        start = this.stage.getRelativePointerPosition()
        if (start) this.placeRect.visible(true)
      },
      move: (e) => {
        if (!start) return
        e.evt.preventDefault()
        const p = this.stage.getRelativePointerPosition()
        if (!p) return
        this.placeRect.setAttrs({
          x: Math.min(start.x, p.x), y: Math.min(start.y, p.y),
          width: Math.abs(p.x - start.x), height: Math.abs(p.y - start.y),
        })
        this.placeLayer.batchDraw()
      },
      up: () => {
        const box = {
          x: this.placeRect.x(), y: this.placeRect.y(),
          w: this.placeRect.width(), h: this.placeRect.height(),
        }
        const done = this.placeCb
        this.cancelPlacement()
        if (!start) return
        if (box.w < 6 || box.h < 6) return // случайный клик — просто отмена
        done?.(box)
      },
      menu: (e) => {
        e.evt.preventDefault()
        this.cancelPlacement()
      },
    }
    this._place = h
    this.stage.on('mousedown touchstart', h.down)
    this.stage.on('mousemove touchmove', h.move)
    this.stage.on('mouseup touchend', h.up)
    this.stage.on('contextmenu', h.menu)
  }

  cancelPlacement() {
    if (!this.placing) return
    this.placing = false
    this.placeCb = null
    const h = this._place
    if (h) {
      this.stage.off('mousedown touchstart', h.down)
      this.stage.off('mousemove touchmove', h.move)
      this.stage.off('mouseup touchend', h.up)
      this.stage.off('contextmenu', h.menu)
    }
    this._place = null
    this.placeLayer?.destroy()
    this.placeLayer = this.placeRect = null
    this.layer.listening(true)
    this.stage.container().style.cursor = ''
    this.layer.batchDraw()
  }

  // Этикетка 1:1 в точках принтера, без рамки выделения.
  renderCanvas() {
    const z = this.zoom
    const sel = this.tr.nodes()
    this.tr.nodes([])
    this.setZoom(1)
    const src = this.stage.toCanvas({ pixelRatio: 1 })
    this.setZoom(z)
    this.tr.nodes(sel)

    const out = document.createElement('canvas')
    out.width = this.W
    out.height = this.H
    const ctx = out.getContext('2d', { willReadFrequently: true })
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, this.W, this.H)
    ctx.drawImage(src, 0, 0)
    return out
  }

  // То, что реально уйдет на принтер.
  renderMono() {
    return canvasToMono(this.renderCanvas())
  }

  destroy() {
    this.cancelPlacement()
    this.stage.destroy()
  }
}
