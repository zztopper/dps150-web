let cached: boolean | null = null

/**
 * Feature-detects a working Canvas 2D context. uPlot draws exclusively
 * on canvas and throws deep inside its render loop when `getContext`
 * returns null — notably in jsdom (unit tests), which has no canvas
 * backend installed. Chart components check this before constructing a
 * `uPlot` instance so they degrade to an empty container instead of
 * crashing the whole test render tree (the crash is asynchronous, via
 * requestAnimationFrame, so it otherwise surfaces as an unrelated
 * "unhandled exception" on a *different*, later test).
 */
export function isCanvas2DSupported(): boolean {
  if (cached !== null) {
    return cached
  }
  try {
    cached = document.createElement('canvas').getContext('2d') !== null
  } catch {
    cached = false
  }
  return cached
}
