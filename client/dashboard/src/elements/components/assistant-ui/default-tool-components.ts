import type { ToolCallMessagePartComponent } from "@assistant-ui/react";

import { ToolSearchResult } from "@/elements/components/assistant-ui/tool-search-result";
import type { DefaultWidgetToolName } from "@/elements/components/assistant-ui/tool-widget-rendering";

/**
 * Tool results Elements renders itself instead of through the generic tool
 * card. Kept in its own module because both the parts registry (thread.tsx)
 * and the run grouping (tool-group.tsx) need it: a tool with a component of
 * its own renders outside the collapsible group, and that check has to see
 * the built-ins as well as the host's overrides.
 *
 * Keyed off `DEFAULT_TOOL_WIDGETS`, so a component and the rule for when it
 * draws cannot drift apart — adding one here without the other fails to
 * compile.
 */
export const DEFAULT_TOOL_COMPONENTS: Record<
  DefaultWidgetToolName,
  ToolCallMessagePartComponent
> = {
  tool_search: ToolSearchResult,
};
