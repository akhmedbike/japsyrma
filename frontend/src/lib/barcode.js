// Штрихкоды через bwip-js. Модуль (самый тонкий элемент) — целое число точек
// принтера: bwip-js рисует 1 модуль = scale пикселей, а пиксель у нас = точка.
import bwipjs from 'bwip-js/browser'

export const SYMBOLOGIES = [
  { id: 'code128', name: 'Code 128', twoD: false, module: 2, sample: 'ABC-12345' },
  { id: 'ean13', name: 'EAN-13', twoD: false, module: 3, sample: '950110153000' },
  { id: 'ean8', name: 'EAN-8', twoD: false, module: 3, sample: '9638507' },
  { id: 'upca', name: 'UPC-A', twoD: false, module: 3, sample: '01234567890' },
  { id: 'code39', name: 'Code 39', twoD: false, module: 2, sample: 'ABC123' },
  { id: 'itf14', name: 'ITF-14', twoD: false, module: 3, sample: '1540014128876' },
  { id: 'gs1-128', name: 'GS1-128', twoD: false, module: 2, sample: '(01)09501101530003' },
  { id: 'qrcode', name: 'QR Code', twoD: true, module: 4, sample: 'https://example.kz' },
  { id: 'datamatrix', name: 'DataMatrix', twoD: true, module: 4, sample: 'ABC123' },
  { id: 'gs1datamatrix', name: 'GS1 DataMatrix', twoD: true, module: 4, sample: '(01)09501101530003(21)ABC123' },
  { id: 'pdf417', name: 'PDF417', twoD: true, module: 2, sample: 'ABC123' },
]

export const SYM = Object.fromEntries(SYMBOLOGIES.map((s) => [s.id, s]))

const POINT_MM = 25.4 / 72
const DPI = 203
const cache = new Map()

// Пунктирная заглушка светло-серым: на экране видна, после порога не печатается.
function placeholder() {
  const c = document.createElement('canvas')
  c.width = 240
  c.height = 80
  const ctx = c.getContext('2d')
  ctx.strokeStyle = '#bbb'
  ctx.setLineDash([8, 6])
  ctx.lineWidth = 3
  ctx.strokeRect(2, 2, 236, 76)
  return c
}

export function renderBarcode(el) {
  const sym = SYM[el.symbology] ?? SYM.code128
  const m = Math.max(1, Math.round(el.module || sym.module))
  const key = JSON.stringify([sym.id, el.value, m, el.barHeight, el.showText, el.textSize])
  const hit = cache.get(key)
  if (hit) return hit

  const opts = {
    bcid: sym.id,
    text: String(el.value ?? ''),
    scale: m,
    paddingwidth: 0,
    paddingheight: 0,
  }
  if (!sym.twoD) {
    // bwip-js: высота в мм при 72 dpi, умножается на scale
    opts.height = Math.max(1, el.barHeight || 80) / m * POINT_MM
    opts.includetext = !!el.showText
    if (el.showText) {
      opts.textxalign = 'center'
      opts.textsize = Math.max(4, ((el.textSize || 8) * DPI) / 72 / m)
    }
  }

  let result
  try {
    if (!opts.text) throw new Error('Пустое значение')
    const canvas = bwipjs.toCanvas(document.createElement('canvas'), opts)
    result = { canvas, error: '' }
  } catch (e) {
    result = { canvas: placeholder(), error: humanError(e) }
  }
  if (cache.size > 300) cache.clear()
  cache.set(key, result)
  return result
}

function humanError(e) {
  const msg = String(e?.message ?? e)
  // bwip-js: "bwipp.ean13badLength#4204: EAN-13 must be 12 or 13 digits"
  const text = msg.includes(': ') ? msg.slice(msg.indexOf(': ') + 2) : msg
  return text
}

export function isValidFor(symbology, value) {
  return !renderBarcode({ symbology, value, module: 1, barHeight: 20, showText: false }).error
}
