import { useEffect, useMemo } from 'react'

interface ParsedShortcut {
  key: string
  meta: boolean
  ctrl: boolean
  shift: boolean
  alt: boolean
}

export function parseShortcut(shortcut: string): ParsedShortcut {
  const parts = shortcut.toLowerCase().split('+')
  return {
    key: parts[parts.length - 1],
    meta: parts.includes('meta') || parts.includes('cmd'),
    ctrl: parts.includes('ctrl') || parts.includes('control'),
    shift: parts.includes('shift'),
    alt: parts.includes('alt') || parts.includes('option'),
  }
}

const IS_MAC =
  typeof navigator !== 'undefined' &&
  navigator.platform.toUpperCase().includes('MAC')

/**
 * Returns a human-readable display string for a shortcut.
 * e.g. "meta+k" → "⌘K" on Mac, "Ctrl+K" on Windows/Linux
 */
export function formatShortcut(shortcut: string): string {
  const parsed = parseShortcut(shortcut)
  const parts: string[] = []

  if (parsed.meta) parts.push(IS_MAC ? '⌘' : 'Ctrl')
  if (parsed.ctrl && !parsed.meta) parts.push(IS_MAC ? '⌃' : 'Ctrl')
  if (parsed.shift) parts.push(IS_MAC ? '⇧' : 'Shift')
  if (parsed.alt) parts.push(IS_MAC ? '⌥' : 'Alt')
  parts.push(parsed.key.toUpperCase())

  return IS_MAC ? parts.join('') : parts.join('+')
}

/**
 * Listens for a keyboard shortcut and calls onTrigger when matched.
 */
export function useCommandBarShortcut(
  shortcut: string,
  onTrigger: () => void
) {
  const parsed = useMemo(() => parseShortcut(shortcut), [shortcut])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const matches =
        e.key.toLowerCase() === parsed.key &&
        e.metaKey === parsed.meta &&
        e.ctrlKey === parsed.ctrl &&
        e.shiftKey === parsed.shift &&
        e.altKey === parsed.alt

      if (matches) {
        e.preventDefault()
        onTrigger()
      }
    }

    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [parsed, onTrigger])
}
