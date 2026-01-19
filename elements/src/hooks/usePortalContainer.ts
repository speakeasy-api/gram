'use client'

import { useContext } from 'react'
import { PortalContainerContext } from '@/contexts/portal-container-context'

/**
 * Because we do not want Tailwind to leak from the Elements library, and
 * because some UI elements such as Dialogs and Tooltips need to be rendered in
 * a different container than the root element, we need to use a portal
 * container, which renders any tooltips, dialogs etc within the .gram-elements
 * scope so that they still inherit the Elements CSS
 */
export function usePortalContainer(): HTMLElement | null {
  const ref = useContext(PortalContainerContext)
  return ref?.current ?? null
}
