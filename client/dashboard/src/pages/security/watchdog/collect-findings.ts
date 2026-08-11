import type { useSdkClient } from "@/contexts/Sdk";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";

// Marking findings false positive from a signal enumerates finding ids
// client-side. The cap keeps a runaway signal (or a large multi-signal
// selection) from turning into thousands of sequential mutations; confirm
// dialogs name the number actually collected.
export const SIGNAL_DISMISS_CAP = 2000;
const PAGE_SIZE = 200;

/**
 * Pages `risk.listResults` for each rule and returns their findings, capped at
 * `cap` overall. The list endpoint's rule filter is substring-match, so an id
 * that is a strict prefix of another could over-fetch; the exact-match filter
 * keeps results scoped to the requested rules only. Pass an empty window to
 * collect regardless of time.
 */
export async function collectFindingsForRules(
  client: ReturnType<typeof useSdkClient>,
  ruleIds: string[],
  window: { from?: Date; to?: Date },
  cap: number = SIGNAL_DISMISS_CAP,
): Promise<RiskResult[]> {
  const all: RiskResult[] = [];
  for (const ruleId of ruleIds) {
    let cursor: string | undefined = undefined;
    do {
      const page = await client.risk.results.list({
        cursor,
        limit: PAGE_SIZE,
        ruleId,
        from: window.from,
        to: window.to,
      });
      all.push(...page.results.filter((result) => result.ruleId === ruleId));
      cursor = page.nextCursor ?? undefined;
    } while (cursor && all.length < cap);
    if (all.length >= cap) break;
  }
  return all.slice(0, cap);
}
