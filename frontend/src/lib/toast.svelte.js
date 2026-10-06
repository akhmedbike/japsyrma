// Всплывающее уведомление внизу окна: видно лучше, чем строка статуса.
export const toast = $state({ text: '', kind: 'ok', visible: false })

let timer = 0

export function showToast(text, kind = 'ok') {
  toast.text = text
  toast.kind = kind
  toast.visible = true
  clearTimeout(timer)
  timer = setTimeout(() => (toast.visible = false), 2600)
}
