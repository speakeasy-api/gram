// Context Providers
export { ElementsProvider as GramElementsProvider } from "./contexts/ElementsProvider";
export { useElements as useGramElements } from "./hooks/useElements";
export { useThreadId } from "./hooks/useThreadId";

// Core Components
export { Chat } from "@/elements/components/Chat";
export { ChatComposer } from "@/elements/components/ChatComposer";
export { focusChatComposer } from "@/elements/lib/composerFocus";
export { ChatHistory } from "@/elements/components/ChatHistory";
export { ActiveChatTitle } from "@/elements/components/ActiveChatTitle";
export { MessageContent } from "@/elements/components/MessageContent";

// Static presentation primitives — render with no ElementsProvider/runtime, so
// the dashboard's chat detail panel can reuse the elements tool UI directly.
export {
  ToolUI,
  ToolUIGroup,
  ToolUISection,
} from "@/elements/components/ui/tool-ui";
export type {
  ToolUIMetaRow,
  SectionMatch,
} from "@/elements/components/ui/tool-ui";

// Frontend Tools
export { defineFrontendTool } from "./lib/tools";
export type { FrontendTool } from "./lib/tools";

// Types
export type {
  ElementsConfig,
  ElementsTransportContext,
  ElementsTransportFactory,
  LinkResolver,
  MCPServerEntry,
  Model,
  ResolvedLink,
  ToolsFilter,
} from "./types";

export { sleep } from "@/lib/utils";
export type { GramChatMessage } from "@/elements/lib/messageConverter";

// Time Range Picker
export {
  TimeRangePicker,
  getPresetRange,
} from "@/elements/components/ui/time-range-picker";
export type {
  TimeRangePickerProps,
  DateRangePreset,
} from "@/elements/components/ui/time-range-picker";
