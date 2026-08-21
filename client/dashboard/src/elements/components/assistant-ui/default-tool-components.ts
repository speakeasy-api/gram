import type { ToolCallMessagePartComponent } from "@assistant-ui/react";

import { ToolSearchResult } from "@/elements/components/assistant-ui/tool-search-result";
import { isCatalogBrowseSearch } from "@/elements/components/assistant-ui/tool-search-result.helpers";

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

/**
 * Whether a built-in draws a widget for a given call, rather than falling
 * through to the generic tool card. A component may serve only some of its
 * tool's calls — `tool_search` draws a catalog only when the user asked for
 * one — and a call it declines must keep the collapsed run the mechanics of a
 * turn belong in. Registered here, next to the components themselves, so the
 * two decisions cannot drift apart.
 *
 * A tool with a component but no predicate always draws.
 */
const DEFAULT_TOOL_WIDGET_PREDICATES: Record<
  string,
  (args: unknown) => boolean
> = {
  tool_search: isCatalogBrowseSearch,
};

export function rendersDefaultToolWidget(
  toolName: string,
  args: unknown,
): boolean {
  if (DEFAULT_TOOL_COMPONENTS[toolName] === undefined) return false;
  const drawsWidget = DEFAULT_TOOL_WIDGET_PREDICATES[toolName];
  return drawsWidget === undefined || drawsWidget(args);
}
