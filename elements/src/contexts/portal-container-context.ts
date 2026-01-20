import { createContext, type RefObject } from 'react'

export const PortalContainerContext =
  createContext<RefObject<HTMLElement | null> | null>(null)
