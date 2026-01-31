'use client'

import { CommandBarDialog } from './command-bar-dialog'

/**
 * CommandBar renders an AI-enabled command palette.
 *
 * Must be used inside a `<CommandBarProvider>`.
 * When also inside an `<ElementsProvider>`, gains AI fallback and MCP tool integration.
 *
 * @example
 * ```tsx
 * import { CommandBar, CommandBarProvider } from '@gram-ai/elements'
 *
 * <CommandBarProvider config={{
 *   shortcut: 'meta+k',
 *   actions: [
 *     { id: 'home', label: 'Go Home', onSelect: () => navigate('/') },
 *   ],
 * }}>
 *   <CommandBar />
 *   {children}
 * </CommandBarProvider>
 * ```
 */
export function CommandBar() {
  return <CommandBarDialog />
}
