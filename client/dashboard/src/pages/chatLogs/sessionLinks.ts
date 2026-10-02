import type { ChatSessionLink } from "@gram/client/models/components/chatsessionlink.js";

/** Per-chat rollup of session-lineage edges, for at-a-glance row indicators. */
export interface LineageSummary {
  /** Harnesses this session was moved to with a captured continuation. */
  continuedIn: string[];
  /** Harnesses this session was moved to whose continuation is not captured
   * (or not visible to the caller — deliberately indistinguishable). */
  danglingIn: string[];
  /** Times this session was recalled into a later session. Recall edges never
   * carry a child chat (the continuation is unknowable at recall time), so
   * they are distinct recall events rather than dangling moves. */
  recalledCount: number;
  /** Whether this session is itself the continuation of an earlier one. */
  derived: boolean;
}

/** Rolls the edges touching one chat into a summary; undefined when the chat
 * has no lineage so callers can render nothing at all. */
export function summarizeLineage(
  links: ChatSessionLink[],
  chatId: string,
): LineageSummary | undefined {
  const summary: LineageSummary = {
    continuedIn: [],
    danglingIn: [],
    recalledCount: 0,
    derived: false,
  };
  for (const link of links) {
    if (link.kind === "subagent") continue;
    if (link.parentChatId === chatId) {
      if (link.kind === "recall") {
        summary.recalledCount += 1;
      } else if (link.childCaptured) {
        summary.continuedIn.push(link.targetHarness);
      } else {
        summary.danglingIn.push(link.targetHarness);
      }
    }
    if (link.childChatId === chatId) {
      summary.derived = true;
    }
  }
  if (
    summary.continuedIn.length === 0 &&
    summary.danglingIn.length === 0 &&
    summary.recalledCount === 0 &&
    !summary.derived
  ) {
    return undefined;
  }
  return summary;
}

/** Stable depth-first order keeps helpers below their parent. Cyclic or
 * incomplete evidence cannot drop rows or loop forever. */
export function groupSubsessions<T extends { id: string }>(
  sessions: T[],
  links: ChatSessionLink[],
): Array<{ session: T; depth: number }> {
  const byId = new Map(sessions.map((session) => [session.id, session]));
  const parentByChild = new Map<string, string>();
  for (const link of links) {
    if (
      link.kind === "subagent" &&
      link.parentChatId &&
      link.childChatId &&
      link.parentChatId !== link.childChatId &&
      byId.has(link.parentChatId) &&
      byId.has(link.childChatId)
    )
      parentByChild.set(link.childChatId, link.parentChatId);
  }
  const children = new Map<string, T[]>();
  for (const session of sessions) {
    const parent = parentByChild.get(session.id);
    if (parent)
      children.set(parent, [...(children.get(parent) ?? []), session]);
  }
  const visited = new Set<string>();
  const ordered: Array<{ session: T; depth: number }> = [];
  const visit = (session: T, depth: number) => {
    if (visited.has(session.id)) return;
    visited.add(session.id);
    ordered.push({ session, depth });
    for (const child of children.get(session.id) ?? []) visit(child, depth + 1);
  };
  for (const session of sessions)
    if (!parentByChild.has(session.id)) visit(session, 0);
  for (const session of sessions) visit(session, 0);
  return ordered;
}
