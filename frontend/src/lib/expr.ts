/** Safe arithmetic evaluator for budget amounts: "=(1368.23*2)+500". Mirrors the Go API. */
export function evalExpr(input: string): number | null {
  const s = input.trim().replace(/^=/, '').replace(/[$,\s]/g, '')
  if (!s) return null
  let i = 0
  let depth = 0
  const fail = (): never => { throw new Error('bad expr') }
  function expr(): number {
    let v = term()
    while (s[i] === '+' || s[i] === '-') {
      const op = s[i++]
      const r = term()
      v = op === '+' ? v + r : v - r
    }
    return v
  }
  function term(): number {
    let v = factor()
    while (s[i] === '*' || s[i] === '/') {
      const op = s[i++]
      const r = factor()
      if (op === '/' && r === 0) fail()
      v = op === '*' ? v * r : v / r
    }
    return v
  }
  function factor(): number {
    const c = s[i]
    if (c === undefined) return fail()
    if (c === '-') { i++; return -factor() }
    if (c === '+') { i++; return factor() }
    if (c === '(') {
      if (++depth > 32) fail()
      i++
      const v = expr()
      if (s[i] !== ')') fail()
      i++
      depth--
      return v
    }
    const m = /^\d*\.?\d+|^\d+\.?/.exec(s.slice(i))
    if (!m) return fail()
    i += m[0].length
    return parseFloat(m[0])
  }
  try {
    const v = expr()
    if (i !== s.length || !Number.isFinite(v)) return null
    return Math.round(v * 100) / 100
  } catch {
    return null
  }
}
