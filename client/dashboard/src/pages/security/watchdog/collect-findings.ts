import type { useSdkClient } from "@/contexts/Sdk";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";

// Marking findings false positive from a signal enumerates finding ids
// client-side. The cap keeps a runaway signal (or a large multi-signal
// selection) from turning into thousands of sequential mutations; confirm
// dialogs name the number actually collected.
export const SIGNAL_DISMISS_CAP = 2000;
const PAGE_SIZE = 200;

type RiskResultsFilter = Omit<
  Parameters<ReturnType<typeof useSdkClient>["risk"]["results"]["list"]>[0],
  "cursor" | "limit"
>;

/** Pages `risk.listResults` until the list ends or `done` returns true.
 * `complete` is false when it stopped early. */
async function pageRiskResults(
  client: ReturnType<typeof useSdkClient>,
  filter: RiskResultsFilter,
  done: (collected: RiskResult[]) => boolean,
): Promise<{ results: RiskResult[]; complete: boolean }> {
  const results: RiskResult[] = [];
  let cursor: string | undefined = undefined;
  do {
    const page = await client.risk.results.list({
      ...filter,
      cursor,
      limit: PAGE_SIZE,
    });
    results.push(...page.results);
    cursor = page.nextCursor ?? undefined;
  } while (cursor && !done(results));
  return { results, complete: !cursor };
}

// Past this many findings a chat is treated as uncollectable.
const CHAT_FINDINGS_CAP = 5000;

/** Every finding in one chat, or null past the cap, so callers never mask from
 * a partial set. */
export async function collectChatFindings(
  client: ReturnType<typeof useSdkClient>,
  chatId: string,
): Promise<RiskResult[] | null> {
  const { results, complete } = await pageRiskResults(
    client,
    { chatId },
    (collected) => collected.length >= CHAT_FINDINGS_CAP,
  );
  return complete ? results : null;
}

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
  window: { from?: Date; to?: Date; mcpServerId?: string },
  cap: number = SIGNAL_DISMISS_CAP,
): Promise<RiskResult[]> {
  const all: RiskResult[] = [];
  for (const ruleId of ruleIds) {
    const matches = (result: RiskResult) => result.ruleId === ruleId;
    const { results } = await pageRiskResults(
      client,
      {
        ruleId,
        from: window.from,
        to: window.to,
        mcpServerId: window.mcpServerId,
      },
      (collected) => all.length + collected.filter(matches).length >= cap,
    );
    all.push(...results.filter(matches));
    if (all.length >= cap) break;
  }
  return all.slice(0, cap);
}
