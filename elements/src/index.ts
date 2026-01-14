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
  AttachmentsConfig,
  COLOR_SCHEMES,
  ColorScheme,
  ComponentOverrides,
  ComposerConfig,
  DENSITIES,
  Density,
  Dimension,
  Dimensions,
  ElementsConfig,
  GetSessionFn,
  HistoryConfig,
  ModalConfig,
  ModalTriggerPosition,
  Model,
  ModelConfig,
  RADII,
  Radius,
  SidecarConfig,
  Suggestion,
  ThemeConfig,
  ToolsConfig,
  Variant,
  VARIANTS,
  WelcomeConfig,
} from './types'

export { MODELS } from './lib/models'

export type { Plugin } from './types/plugins'
