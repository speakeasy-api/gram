import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import type { LogFilter } from "@gram/client/models/components/logfilter.js";
import { Operator } from "@gram/client/models/components/logfilter";

/**
 * How a skill reached an agent: read over MCP from a server or gateway that
 * serves it, or reported by an agent hook when the agent ran its Skill tool.
 */
export type SkillLoadChannel = "mcp" | "hook";

/** The attribute that tells the two apart: hook rows say "hook". */
export const EVENT_SOURCE_ATTRIBUTE_PATH = "gram.event.source";
const HOOK_EVENT_SOURCE = "hook";

export const SKILL_LOAD_CHANNEL_LABELS: Record<SkillLoadChannel, string> = {
  mcp: "MCP",
  hook: "Hook",
};

/**
 * The channel a trace row loaded its skill through, or null for a row that is
 * not a skill load. Anything a hook did not report came in over MCP.
 */
export function skillLoadChannel(trace: {
  targetType: string;
  eventSource: string;
}): SkillLoadChannel | null {
  if (trace.targetType !== "skill") return null;
  return trace.eventSource === HOOK_EVENT_SOURCE ? "hook" : "mcp";
}

/**
 * The attribute filter that narrows rows to one channel. "MCP" is "not a
 * hook" rather than an exact source, so it keeps matching whatever source the
 * server stamps on an MCP skill read.
 */
function loadedViaFilter(channel: SkillLoadChannel): {
  path: string;
  op: Operator;
  value: string;
} {
  return {
    path: EVENT_SOURCE_ATTRIBUTE_PATH,
    op: channel === "hook" ? Operator.Eq : Operator.NotEq,
    value: HOOK_EVENT_SOURCE,
  };
}

/** The same filter in the shape the telemetry payloads take. */
export function loadedViaLogFilter(channel: SkillLoadChannel): LogFilter {
  const filter = loadedViaFilter(channel);
  return {
    path: filter.path,
    operator: filter.op,
    values: [filter.value],
  };
}

/**
 * The channel the applied filters select, or null when they select neither or
 * something this control did not write (any other chip on the path).
 */
export function selectedLoadedVia(
  filters: ActiveLogFilter[],
): SkillLoadChannel | null {
  const onPath = filters.filter((f) => f.path === EVENT_SOURCE_ATTRIBUTE_PATH);
  const [filter] = onPath;
  if (onPath.length !== 1 || !filter) return null;
  if (filter.value !== HOOK_EVENT_SOURCE) return null;
  if (filter.op === Operator.Eq) return "hook";
  if (filter.op === Operator.NotEq) return "mcp";
  return null;
}

/**
 * Replaces any event-source chip with the one for `channel`, or clears it.
 * Replacing rather than adding: an eq hook and a not_eq hook together match
 * nothing.
 */
export function withLoadedVia(
  filters: ActiveLogFilter[],
  channel: SkillLoadChannel | null,
): ActiveLogFilter[] {
  const rest = filters.filter((f) => f.path !== EVENT_SOURCE_ATTRIBUTE_PATH);
  if (!channel) return rest;
  return [...rest, { id: crypto.randomUUID(), ...loadedViaFilter(channel) }];
}

export type SkillLoadSplit = { total: number; mcp: number; hook: number };

/**
 * Splits each skill's load count into MCP and hook parts, given the hook-only
 * count per skill from a second, hook-filtered read. Whatever a hook did not
 * report came in over MCP; the clamp absorbs the two reads disagreeing at a
 * window edge.
 */
export function splitSkillLoads(
  skills: Array<{ targetId: string; eventCount: number }>,
  hookCounts: ReadonlyMap<string, number>,
): { bySkill: Map<string, SkillLoadSplit>; totals: SkillLoadSplit } {
  const bySkill = new Map<string, SkillLoadSplit>();
  const totals: SkillLoadSplit = { total: 0, mcp: 0, hook: 0 };
  for (const skill of skills) {
    const total = skill.eventCount;
    const hook = Math.min(hookCounts.get(skill.targetId) ?? 0, total);
    const split = { total, mcp: total - hook, hook };
    bySkill.set(skill.targetId, split);
    totals.total += split.total;
    totals.mcp += split.mcp;
    totals.hook += split.hook;
  }
  return { bySkill, totals };
}
