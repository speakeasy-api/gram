// Side-effect import to include CSS in build (consumers import via @gram-ai/elements/elements.css)
import './global.css'

// Context Providers
export { ElementsProvider as GramElementsProvider } from './contexts/ElementsProvider'
export { useElements as useGramElements } from './hooks/useElements'

// Core Components
export { Chat } from '@/components/Chat'

// Frontend Tools
export { defineFrontendTool } from './lib/tools'
export type { FrontendTool } from './lib/tools'

// Types
export type {
  ElementsProviderProps,
  ElementsConfig,
  ComposerConfig,
  AttachmentsConfig,
  ModalConfig,
  SidecarConfig,
  ToolsConfig,
  ModelConfig,
  ThemeConfig,
  WelcomeConfig,
  Suggestion,
  Model,
  ModalTriggerPosition,
  ColorScheme,
  Radius,
  Density,
  Variant,
  Dimensions,
  Dimension,
  ComponentOverrides,
} from './types'

export type { Plugin } from './types/plugins'
