import { useEffect } from 'react'

/**
 * Sets `document.title` to "<title> · DPS-150" while the calling component
 * is mounted, restoring the previous title on unmount. Gives each route a
 * distinct, bookmarkable browser-tab title (the static index.html default,
 * "DPS-150 Control", still applies before the app mounts / for unknown
 * routes). Re-runs when `title` changes, e.g. on a language switch.
 */
export function usePageTitle(title: string): void {
  useEffect(() => {
    const previous = document.title
    document.title = `${title} · DPS-150`
    return () => {
      document.title = previous
    }
  }, [title])
}
