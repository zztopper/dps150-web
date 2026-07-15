import { type RefObject, useEffect, useState } from 'react'

export interface ContainerSize {
  width: number
  height: number
}

/**
 * Tracks the content-box size of `ref.current` via ResizeObserver, for
 * charts that must fill their container (dashboard card, page section).
 * Returns `{width: 0, height: 0}` until the first observation.
 */
export function useContainerSize(
  ref: RefObject<HTMLElement | null>,
): ContainerSize {
  const [size, setSize] = useState<ContainerSize>({ width: 0, height: 0 })

  useEffect(() => {
    const el = ref.current
    // jsdom (unit tests) has no ResizeObserver; charts still mount and
    // render at their initial size, they just don't track live resizes.
    if (el === null || typeof ResizeObserver === 'undefined') {
      return
    }
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry === undefined) {
        return
      }
      const { width, height } = entry.contentRect
      setSize((prev) =>
        prev.width === width && prev.height === height
          ? prev
          : { width, height },
      )
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [ref])

  return size
}
