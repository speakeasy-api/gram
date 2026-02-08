import { ElementsContext } from '@/contexts/contexts'
import type { ReactNode } from 'react'

// Minimal mock config for stories that only need theme hooks (useRadius, useDensity)
const mockElementsContext = {
  config: {
    projectSlug: 'storybook',
    mcp: 'mock',
    theme: {
      radius: 'soft' as const,
      density: 'normal' as const,
    },
  },
  runtime: null,
}

/**
 * Minimal wrapper for generative UI component stories.
 * Provides just enough context for useRadius and useDensity hooks.
 */
export const GenerativeUIWrapper = ({ children }: { children: ReactNode }) => (
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  <ElementsContext.Provider value={mockElementsContext as any}>
    {children}
  </ElementsContext.Provider>
)
