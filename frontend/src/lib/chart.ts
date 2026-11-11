export interface Pt { x: number; y: number }

/** Round a max value up to a "nice" axis maximum and return evenly spaced ticks. */
export function niceScale(max: number, count = 5): { max: number; ticks: number[] } {
  if (!(max > 0)) return { max: 1, ticks: [0, 1] }
  const rough = max / count
  const mag = Math.pow(10, Math.floor(Math.log10(rough)))
  const norm = rough / mag
  const step = (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 2.5 ? 2.5 : norm <= 5 ? 5 : 10) * mag
  const top = Math.ceil(max / step) * step
  const ticks: number[] = []
  for (let v = 0; v <= top + step / 1000; v += step) ticks.push(v)
  return { max: top, ticks }
}

/** Monotone cubic (Fritsch–Carlson) path through screen points — smooth without overshoot. */
export function monotonePath(p: Pt[]): string {
  const n = p.length
  if (n === 0) return ''
  if (n === 1) return `M${p[0].x},${p[0].y}`
  const dx: number[] = [], m: number[] = [], t: number[] = new Array(n).fill(0)
  for (let i = 0; i < n - 1; i++) {
    dx[i] = p[i + 1].x - p[i].x || 1e-6
    m[i] = (p[i + 1].y - p[i].y) / dx[i]
  }
  t[0] = m[0]
  t[n - 1] = m[n - 2]
  for (let i = 1; i < n - 1; i++) t[i] = m[i - 1] * m[i] <= 0 ? 0 : (m[i - 1] + m[i]) / 2
  for (let i = 0; i < n - 1; i++) {
    if (m[i] === 0) { t[i] = 0; t[i + 1] = 0; continue }
    const a = t[i] / m[i], b = t[i + 1] / m[i], s = a * a + b * b
    if (s > 9) { const k = 3 / Math.sqrt(s); t[i] = k * a * m[i]; t[i + 1] = k * b * m[i] }
  }
  let d = `M${p[0].x},${p[0].y}`
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i] / 3
    d += ` C${p[i].x + h},${p[i].y + t[i] * h} ${p[i + 1].x - h},${p[i + 1].y - t[i + 1] * h} ${p[i + 1].x},${p[i + 1].y}`
  }
  return d
}

export function nearestIndex(xs: number[], x: number): number {
  let lo = 0, hi = xs.length - 1
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1
    if (xs[mid] < x) lo = mid; else hi = mid
  }
  return Math.abs(xs[lo] - x) <= Math.abs(xs[hi] - x) ? lo : hi
}
