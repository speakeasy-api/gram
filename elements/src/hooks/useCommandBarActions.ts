import { useEffect, type DependencyList } from 'react'
import { useCommandBar } from '@/contexts/CommandBarContext'
import type { CommandBarAction } from '@/types'

/**
 * Convenience hook that registers actions with the CommandBar
 * and automatically unregisters them on unmount or when deps change.
 *
 * @example
 * ```tsx
 * useCommandBarActions([
 *   {
 *     id: 'toggle-theme',
 *     label: 'Toggle Dark Mode',
 *     group: 'Appearance',
 *     onSelect: () => toggleTheme(),
 *   },
 * ])
 * ```
 */
export function useCommandBarActions(
  actions: CommandBarAction[],
  deps: DependencyList = []
) {
  const { registerActions } = useCommandBar()

  useEffect(() => {
    return registerActions(actions)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
}
