import type { ChatSessionLink } from "@gram/client/models/components/chatsessionlink.js";

/** Per-chat rollup of session-lineage edges, for at-a-glance row indicators. */
export interface LineageSummary {
  /** Harnesses this session was moved to with a captured continuation. */
  continuedIn: string[];
  /** Harnesses this session was moved to whose continuation is not captured
   * (or not visible to the caller — deliberately indistinguishable). */
  danglingIn: string[];
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
    derived: false,
  };
  for (const link of links) {
    if (link.parentChatId === chatId) {
      if (link.childCaptured) {
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
    !summary.derived
  ) {
    return undefined;
  }
  return summary;
}
