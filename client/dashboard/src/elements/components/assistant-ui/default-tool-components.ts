import type { ToolCallMessagePartComponent } from "@assistant-ui/react";

import { ToolSearchResult } from "@/elements/components/assistant-ui/tool-search-result";

/**
 * Tool results Elements renders itself instead of through the generic tool
 * card. Kept in its own module because both the parts registry (thread.tsx)
 * and the run grouping (tool-group.tsx) need it: a tool with a component of
 * its own renders outside the collapsible group, and that check has to see
 * the built-ins as well as the host's overrides.
 */
export const DEFAULT_TOOL_COMPONENTS: Record<
  string,
  ToolCallMessagePartComponent
> = {
  tool_search: ToolSearchResult,
};
