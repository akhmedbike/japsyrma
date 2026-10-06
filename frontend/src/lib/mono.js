// Перевод изображений в 1 бит: термоголовка умеет только «точка есть / точки нет».

export function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('Не удалось открыть изображение'))
    img.src = src
  })
}

const clamp = (v) => (v < 0 ? 0 : v > 255 ? 255 : v)

function toGray(img, w, h) {
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  const ctx = c.getContext('2d', { willReadFrequently: true })
  ctx.fillStyle = '#fff' // прозрачность -> белый
  ctx.fillRect(0, 0, w, h)
  ctx.imageSmoothingEnabled = true
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(img, 0, 0, w, h)
  const d = ctx.getImageData(0, 0, w, h).data
  const g = new Float32Array(w * h)
  for (let i = 0, p = 0; i < g.length; i++, p += 4) {
    g[i] = 0.299 * d[p] + 0.587 * d[p + 1] + 0.114 * d[p + 2]
  }
  return g
}

function threshold(g, t) {
  const out = new Uint8Array(g.length)
  for (let i = 0; i < g.length; i++) out[i] = g[i] < t ? 1 : 0
  return out
}

// Floyd–Steinberg
function dither(g, w, h) {
  const v = Float32Array.from(g)
  const out = new Uint8Array(g.length)
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const i = y * w + x
      const old = v[i]
      let nw = 255
      if (old < 128) {
        nw = 0
        out[i] = 1
      }
      const e = old - nw
      if (x + 1 < w) v[i + 1] += (e * 7) / 16
      if (y + 1 < h) {
        if (x > 0) v[i + w - 1] += (e * 3) / 16
        v[i + w] += (e * 5) / 16
        if (x + 1 < w) v[i + w + 1] += e / 16
      }
    }
  }
  return out
}

// Черные точки непрозрачны, белые — прозрачны, чтобы элементы
// могли перекрываться так же, как при печати.
function maskToCanvas(black, w, h) {
  const c = document.createElement('canvas')
  c.width = w
  c.height = h
  const ctx = c.getContext('2d')
  const id = ctx.createImageData(w, h)
  for (let i = 0, p = 0; i < black.length; i++, p += 4) {
    if (black[i]) id.data[p + 3] = 255 // rgb уже 0
  }
  ctx.putImageData(id, 0, 0)
  return c
}

export function processImage(img, width, height, o) {
  const w = Math.max(1, Math.round(width))
  const h = Math.max(1, Math.round(height))
  const g = toGray(img, w, h)
  const contrast = o.contrast ?? 1
  const brightness = o.brightness ?? 0
  for (let i = 0; i < g.length; i++) g[i] = clamp((g[i] - 128) * contrast + 128 + brightness)
  const black = o.mode === 'threshold' ? threshold(g, o.threshold ?? 128) : dither(g, w, h)
  if (o.invert) for (let i = 0; i < black.length; i++) black[i] ^= 1
  return maskToCanvas(black, w, h)
}

// Финальный порог всей этикетки: убирает серые края сглаженного текста.
export function canvasToMono(canvas) {
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  const id = ctx.getImageData(0, 0, canvas.width, canvas.height)
  const d = id.data
  for (let p = 0; p < d.length; p += 4) {
    const a = d[p + 3] / 255
    const l = (0.299 * d[p] + 0.587 * d[p + 1] + 0.114 * d[p + 2]) * a + 255 * (1 - a)
    const v = l < 128 ? 0 : 255
    d[p] = d[p + 1] = d[p + 2] = v
    d[p + 3] = 255
  }
  ctx.putImageData(id, 0, 0)
  return canvas
}
