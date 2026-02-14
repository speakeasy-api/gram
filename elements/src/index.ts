// Polyfill React 18 APIs for older React versions — must be the first import
import './compat'

// Side-effect import to include CSS in build (consumers import via @gram-ai/elements/elements.css)
import './global.css'

// Context Providers
export { ElementsProvider as GramElementsProvider } from './contexts/ElementsProvider'
export { ElementsProvider } from './contexts/ElementsProvider'
export { useElements as useGramElements } from './hooks/useElements'
export { useElements } from './hooks/useElements'
export { useThreadId } from './hooks/useThreadId'
export { useChatId } from './contexts/ChatIdContext'

// Core Components
export { Chat } from '@/components/Chat'
export { ChatHistory } from '@/components/ChatHistory'
export { ShareButton } from '@/components/ShareButton'
export type { ShareButtonProps } from '@/components/ShareButton'

// Replay
export { Replay } from '@/components/Replay'
export { useRecordCassette } from '@/hooks/useRecordCassette'
export type {
  Cassette,
  CassetteMessage,
  CassettePart,
  ReplayOptions,
} from '@/lib/cassette'

// Frontend Tools
export { defineFrontendTool } from './lib/tools'
export type { FrontendTool } from './lib/tools'

// Error Tracking
export { trackError } from './lib/errorTracking'
export type { ErrorContext } from './lib/errorTracking'

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
  ErrorTrackingConfigOption,
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
  ToolMentionsConfig,
  ToolsConfig,
  Variant,
  VARIANTS,
  WelcomeConfig,
} from './types'

export { MODELS } from './lib/models'

export type { Plugin } from './types/plugins'

// Time Range Picker
export { TimeRangePicker } from '@/components/ui/time-range-picker'
export type {
  TimeRange,
  TimeRangePreset,
  TimeRangePickerProps,
} from '@/components/ui/time-range-picker'
export { useTimeRange } from '@/hooks/useTimeRange'
export type {
  UseTimeRangeOptions,
  UseTimeRangeReturn,
} from '@/hooks/useTimeRange'
export { Calendar } from '@/components/ui/calendar'
export type { CalendarProps } from '@/components/ui/calendar'
export {
  parseTimeRange,
  formatTimeRange,
  describeTimeRange,
  applyPreset,
  DEFAULT_PRESETS,
} from '@/lib/time-parser'
