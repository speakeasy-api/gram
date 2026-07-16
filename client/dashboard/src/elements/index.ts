// Context Providers
export { ElementsProvider as GramElementsProvider } from "./contexts/ElementsProvider";
export { ElementsProvider } from "./contexts/ElementsProvider";
export { useElements as useGramElements } from "./hooks/useElements";
export { useElements } from "./hooks/useElements";
export { useThreadId } from "./hooks/useThreadId";
export { useChatId } from "./contexts/ChatIdContext";
export {
  MarkdownLinkProvider,
  useMarkdownLink,
} from "./contexts/MarkdownLinkContext";
export type { MarkdownLinkValue } from "./contexts/MarkdownLinkContext";

// Core Components
export { Chat } from "@/elements/components/Chat";
export { ChatHistory } from "@/elements/components/ChatHistory";
export { ActiveChatTitle } from "@/elements/components/ActiveChatTitle";
export { ShareButton } from "@/elements/components/ShareButton";
export type { ShareButtonProps } from "@/elements/components/ShareButton";
export { ToolFallback } from "@/elements/components/assistant-ui/tool-fallback";
export { MessageContent } from "@/elements/components/MessageContent";
export type { MessageContentProps } from "@/elements/components/MessageContent";
export { Markdown } from "@/elements/components/Markdown";
export type { MarkdownProps } from "@/elements/components/Markdown";

// Static presentation primitives — render with no ElementsProvider/runtime, so
// the dashboard's chat detail panel can reuse the elements tool UI directly.
export {
  ToolUI,
  ToolUIGroup,
  ToolUISection,
  SyntaxHighlightedCode,
} from "@/elements/components/ui/tool-ui";
export type {
  ToolUIProps,
  ToolUIGroupProps,
  ToolUISectionProps,
  ToolStatus,
  ContentItem,
  SectionHighlight,
  SectionMatch,
} from "@/elements/components/ui/tool-ui";

// Replay
export { Replay } from "@/elements/components/Replay";
export { useRecordCassette } from "@/elements/hooks/useRecordCassette";
export type {
  Cassette,
  CassetteMessage,
  CassettePart,
  ReplayOptions,
} from "@/elements/lib/cassette";

// Frontend Tools
export { defineFrontendTool } from "./lib/tools";
export type { FrontendTool } from "./lib/tools";

// Error Tracking
export { trackError } from "./lib/errorTracking";
export type { ErrorContext } from "./lib/errorTracking";
export {
  CREDITS_EXHAUSTED_MESSAGE,
  describeStreamError,
} from "./lib/streamErrorMessage";

// Types
export type {
  AttachmentsConfig,
  COLOR_SCHEMES,
  ColorScheme,
  ComponentOverrides,
  ComposerConfig,
  DangerousApiKeyAuthConfig,
  DENSITIES,
  Density,
  Dimension,
  Dimensions,
  ElementsConfig,
  ElementsTransportContext,
  ElementsTransportFactory,
  ErrorTrackingConfigOption,
  GetSessionFn,
  HistoryConfig,
  LinkResolver,
  MarkdownLinkComponent,
  MCPServerEntry,
  ModalConfig,
  ModalTriggerPosition,
  Model,
  ModelConfig,
  RADII,
  Radius,
  ResolvedLink,
  SidecarConfig,
  Suggestion,
  ThemeConfig,
  ToolMentionsConfig,
  ToolsConfig,
  ToolsFilter,
  UnifiedSessionAuthConfig,
  Variant,
  VARIANTS,
  WelcomeConfig,
} from "./types";

export { MODELS } from "./lib/models";

// Chat-message conversion — for consumers building a custom transport against
// the Gram chat service (e.g. the dashboard's server-assistant transport).
export {
  convertGramMessagesToUIMessages,
  convertGramMessagesToExported,
} from "@/elements/lib/messageConverter";

export { sleep } from "@/lib/utils";
export type {
  GramChat,
  GramChatMessage,
  GramChatOverview,
} from "@/elements/lib/messageConverter";

export type { Plugin } from "./types/plugins";

// Time Range Picker
export {
  TimeRangePicker,
  getPresetRange,
  PRESETS,
} from "@/elements/components/ui/time-range-picker";
export type {
  TimeRange,
  TimeRangePreset,
  TimeRangePickerProps,
  DateRangePreset,
} from "@/elements/components/ui/time-range-picker";
export { Calendar } from "@/elements/components/ui/calendar";
export type { CalendarProps } from "@/elements/components/ui/calendar";
