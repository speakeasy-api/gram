import { groupAssistantMessageParts } from "@/elements/lib/messagePartGrouping";
import { isCatalogBrowseSearch } from "@/elements/components/assistant-ui/tool-search-result.helpers";

/**
 * Which of a tool's calls Elements' own card actually draws for. A built-in
 * may serve only some of them — `tool_search` draws a catalog only when the
 * user asked for one — and a call it declines belongs in the collapsed run
 * with the rest of a turn's mechanics.
 *
 * `DEFAULT_TOOL_COMPONENTS` is typed off this registry's keys, so a built-in
 * cannot gain a component without an entry here, or keep one after losing it.
 */
export const DEFAULT_TOOL_WIDGETS = {
  tool_search: isCatalogBrowseSearch,
} satisfies Record<string, (args: unknown) => boolean>;

export type DefaultWidgetToolName = keyof typeof DEFAULT_TOOL_WIDGETS;

/** The parts of a message, as much of one as any of this needs to read. */
interface ToolPartLike {
  readonly type: string;
  readonly toolCallId?: string;
  readonly toolName?: string;
  readonly args?: unknown;
  readonly text?: string;
}

type HostToolComponents = Record<string, unknown> | undefined;

/**
 * `Object.hasOwn` rather than a lookup against `undefined`: these are plain
 * records keyed by a tool name the model chose, and a server is free to serve
 * a tool called `toString` or `constructor`. Reading those off the prototype
 * would report a component that does not exist.
 */
function hasComponent(map: Record<string, unknown> | undefined, name: string) {
  return (
    map !== undefined && Object.hasOwn(map, name) && map[name] !== undefined
  );
}

function partDrawsWidget(
  part: ToolPartLike,
  hostComponents: HostToolComponents,
): boolean {
  const name = part.toolName ?? "";
  // A host override is taken at its word; a built-in is asked, since it may
  // decline this particular call.
  if (hasComponent(hostComponents, name)) return true;
  if (!Object.hasOwn(DEFAULT_TOOL_WIDGETS, name)) return false;
  return DEFAULT_TOOL_WIDGETS[name as DefaultWidgetToolName](part.args);
}

/**
 * Whether every call in a run draws a card of its own — the rule `ToolGroup`
 * hoists on. A run that clears it renders in the message body, because
 * collapsing it would only hide the answer; anything else keeps the
 * collapsible that hides a turn's mechanics.
 */
export function runDrawsEveryWidget(
  parts: readonly ToolPartLike[],
  indices: readonly number[],
  hostComponents: HostToolComponents,
): boolean {
  let sawToolCall = false;
  for (const i of indices) {
    const part = parts[i];
    if (part?.type !== "tool-call") continue;
    sawToolCall = true;
    if (!partDrawsWidget(part, hostComponents)) return false;
  }
  return sawToolCall;
}

/**
 * What one `tool_search` call should render.
 *
 * - `draw` — the catalog card. At most one call in a message gets this.
 * - `suppress` — nothing. A model often searches several times before it
 *   answers and every search carries the same whole-catalog view, so the
 *   duplicates render nothing rather than stacking identical cards. Only a
 *   call that would otherwise have drawn is suppressed; a hidden fallback row
 *   would be no loss, but a fallback card hoisted into the message body is.
 * - `fallback` — the generic tool card, which is the answer for a discovery
 *   search and for a browse whose run cannot be hoisted.
 *
 * The verdict is read from the message's own parts, grouped exactly as
 * `MessagePrimitive.Unstable_PartsGrouped` groups them, because a call can
 * only see its own props: which browse draws is a question about the run it
 * landed in and about every other browse in the message.
 */
export type ToolSearchVerdict = "draw" | "suppress" | "fallback";

export function toolSearchVerdict(
  parts: readonly ToolPartLike[],
  toolCallId: string,
  hostComponents: HostToolComponents,
): ToolSearchVerdict {
  let drawn: string | undefined;
  let ownRunDraws = false;
  let found = false;

  for (const group of groupAssistantMessageParts(parts)) {
    const runDraws = runDrawsEveryWidget(parts, group.indices, hostComponents);
    for (const i of group.indices) {
      const part = parts[i];
      if (part?.type !== "tool-call") continue;
      if (part.toolCallId === toolCallId) {
        found = true;
        ownRunDraws = runDraws;
      }
      // Only a browse the group would hoist can hold the card. A browse
      // stranded in a collapsed run does not claim it and does not deny it to
      // an earlier one that can draw.
      if (
        runDraws &&
        part.toolName === "tool_search" &&
        isCatalogBrowseSearch(part.args)
      ) {
        drawn = part.toolCallId;
      }
    }
  }

  if (found && drawn === toolCallId) return "draw";
  return ownRunDraws && drawn !== undefined ? "suppress" : "fallback";
}
