import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

export const prefersReducedMotion = () =>
  typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

/** Smoothly tweens a number toward the target whenever it changes. */
export function useTween(target: Ref<number> | (() => number), duration = 900) {
  const get = typeof target === 'function' ? target : () => target.value
  const shown = ref(0)
  let raf = 0
  const run = (to: number) => {
    cancelAnimationFrame(raf)
    if (prefersReducedMotion()) { shown.value = to; return }
    const from = shown.value
    const t0 = performance.now()
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / duration)
      const e = 1 - Math.pow(1 - p, 4) // easeOutQuart
      shown.value = from + (to - from) * e
      if (p < 1) raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
  }
  watch(get, (v) => run(v), { immediate: true })
  onBeforeUnmount(() => cancelAnimationFrame(raf))
  return shown
}
