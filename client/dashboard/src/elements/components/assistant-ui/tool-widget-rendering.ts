import { groupAssistantMessageParts } from "@/elements/lib/messagePartGrouping";
import {
  catalogPayload,
  isCatalogBrowseSearch,
} from "@/elements/components/assistant-ui/tool-search-result.helpers";

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
  readonly result?: unknown;
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

/**
 * Verdicts for every `tool_search` call in a message, memoised on the parts
 * array.
 *
 * One walk serves every card in the message: computing this per card would
 * re-group the parts and re-read every browse's catalog once per card, and a
 * streaming message hands back a fresh parts array on every token. Keyed
 * weakly on that array — safe because the state's parts are replaced rather
 * than mutated in place, so a given array always describes the same message.
 */
const verdictsByParts = new WeakMap<
  object,
  {
    hostComponents: HostToolComponents;
    verdicts: ReadonlyMap<string, ToolSearchVerdict>;
  }
>();

export function toolSearchVerdict(
  parts: readonly ToolPartLike[],
  toolCallId: string,
  hostComponents: HostToolComponents,
): ToolSearchVerdict {
  const cached = verdictsByParts.get(parts);
  const verdicts =
    cached && cached.hostComponents === hostComponents
      ? cached.verdicts
      : computeVerdicts(parts, hostComponents);
  if (verdicts !== cached?.verdicts) {
    verdictsByParts.set(parts, { hostComponents, verdicts });
  }
  // A call this message does not contain gets the generic card, same as one
  // whose run cannot be hoisted.
  return verdicts.get(toolCallId) ?? "fallback";
}

function computeVerdicts(
  parts: readonly ToolPartLike[],
  hostComponents: HostToolComponents,
): ReadonlyMap<string, ToolSearchVerdict> {
  const searches: { id: string; runDraws: boolean; drawable: boolean }[] = [];

  for (const group of groupAssistantMessageParts(parts)) {
    const runDraws = runDrawsEveryWidget(parts, group.indices, hostComponents);
    for (const i of group.indices) {
      const part = parts[i];
      if (part?.type !== "tool-call" || part.toolName !== "tool_search") {
        continue;
      }
      if (part.toolCallId === undefined) continue;
      // Only a browse the group would hoist, and whose result is a catalog the
      // card can actually render, is a candidate to draw. A browse that is
      // stranded in a collapsed run, still running, or back with an unreadable
      // result neither claims the card nor denies it to one that can draw —
      // otherwise a second search would blank the catalog the first had
      // already put on screen.
      const drawable =
        runDraws &&
        isCatalogBrowseSearch(part.args) &&
        catalogPayload(part.result) !== null;
      searches.push({ id: part.toolCallId, runDraws, drawable });
    }
  }

  const drawn = searches.findLast((search) => search.drawable)?.id;
  const verdicts = new Map<string, ToolSearchVerdict>();
  for (const search of searches) {
    if (search.id === drawn) {
      verdicts.set(search.id, "draw");
      continue;
    }
    // A hoisted search that is not the chosen one renders nothing only while
    // another one is drawing: a lone browse still waiting on its result keeps
    // the generic card, which is where its running state shows.
    verdicts.set(
      search.id,
      search.runDraws && drawn !== undefined ? "suppress" : "fallback",
    );
  }
  return verdicts;
}
