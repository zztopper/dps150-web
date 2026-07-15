import { useEffect, useState } from 'react'

/**
 * Tracks `document.visibilityState`, so charts can pause redraw work
 * (and, for the live buffer, sample accumulation) while the tab is in
 * the background.
 */
export function usePageVisible(): boolean {
  const [visible, setVisible] = useState(
    () => document.visibilityState === 'visible',
  )

  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', onChange)
    return () => document.removeEventListener('visibilitychange', onChange)
  }, [])

  return visible
}
