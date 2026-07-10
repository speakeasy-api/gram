import { type ReactNode, useEffect, useRef, useState } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { cn } from "@/lib/utils";
import { ruleIdCategoryLabel } from "@/pages/security/rule-ids";
import { serializeExclusionExpression } from "@/pages/security/exclusion-expression";
import {
  type ExclusionSheetState,
  GLOBAL_SCOPE,
} from "@/pages/security/exclusion-sheet";
import {
  getCategoryCodeForFinding,
  getRuleTitleFallback,
} from "@/pages/security/risk-utils";
import { useRevealAll } from "@/pages/security/reveal-all-context";

/** Soft warning (yellow) wash for a *non-sensitive* flagged span — always shown,
 * so it keeps the natural proportional text. */
const RISK_MARK_CLASS =
  "bg-warning-softest px-0.5 text-foreground ring-1 ring-warning-softest";

/** A sensitive span toggles between dotted-out and revealed, so both states must
 * occupy the exact same width or revealing reflows the message. Shared box +
 * monospace (so char-count == width); destructive tint matching the risk divider,
 * with a stronger wash once revealed to signal the value is exposed. */
const SENSITIVE_MARK_BASE = "px-0.5 font-mono ring-1";
const SENSITIVE_MARK_MASKED =
  "bg-destructive/10 text-destructive ring-destructive/20";
const SENSITIVE_MARK_REVEALED =
  "bg-destructive/15 text-destructive ring-destructive/30";

export function getRiskBadgeLabel(result: RiskResult): string {
  if (result.ruleId === "llm_judge") return getRuleTitleFallback(result.ruleId);
  return (
    ruleIdCategoryLabel(result.ruleId) ||
    getCategoryCodeForFinding(result.source, result.ruleId)
  );
}

export function shouldShowRiskRuleId(result: RiskResult): boolean {
  return Boolean(result.ruleId) && result.ruleId !== "llm_judge";
}

/** A finding from gitleaks/presidio carries a literal secret; its match is
 * masked until the viewer explicitly reveals it. */
export function resultsAreSensitive(
  results: RiskResult[] | undefined,
): boolean {
  return (
    results?.some((r) => r.source === "gitleaks" || r.source === "presidio") ??
    false
  );
}

/** Count of distinct findings (by source/rule/match), matching the RiskBadge's
 * grouping — used for the "N risks" turn-divider label. */
export function distinctRiskCount(results: RiskResult[]): number {
  const keys = new Set<string>();
  for (const r of results) {
    keys.add(`${r.source} ${r.ruleId ?? ""} ${r.match ?? ""}`);
  }
  return keys.size;
}

/**
 * Per-source overrides for how a finding's `match` is rendered. By default a
 * match is a span lifted from the message the reviewer is reading, so the UI
 * highlights it inline (or surfaces it as an out-of-text "flagged value" when it
 * was stripped for display) and repeats it as a reveal-gated chip in the
 * findings popover. Some policy types instead match on session/account metadata
 * that isn't message content and is already stated in the finding description,
 * making those renderings pure redundancy.
 *
 * To adjust a policy type's match rendering, add its source here — the two flags
 * are independent, so a future type opts into only what applies to it.
 */
type MatchDisplayOverride = {
  /** The match isn't drawn from the message text: don't highlight it inline or
   * surface it as an out-of-text "flagged value" annotation. */
  notMessageContent?: boolean;
  /** The match already appears in the finding description: skip the reveal-gated
   * chip in the findings popover that would just repeat it. */
  shownInDescription?: boolean;
};

const MATCH_DISPLAY_OVERRIDES: Record<
  string,
  MatchDisplayOverride | undefined
> = {
  // The account the session authenticated as (an email): stated in the
  // description and shown on the message author chip, never message content.
  account_identity: { notMessageContent: true, shownInDescription: true },
};

/** Whether a finding's match is a span of the message text — highlighted inline
 * and eligible for the out-of-text "flagged value" annotation. False for
 * metadata matches like the authenticated account email. */
function matchIsMessageContent(result: RiskResult): boolean {
  return !MATCH_DISPLAY_OVERRIDES[result.source]?.notMessageContent;
}

/** Whether a finding's match is already conveyed by its description, so the
 * findings popover should not repeat it as a separate reveal-gated chip. */
export function matchShownInDescription(result: RiskResult): boolean {
  return Boolean(MATCH_DISPLAY_OVERRIDES[result.source]?.shownInDescription);
}

/** Distinct, non-empty match strings to highlight, longest first so a longer
 * secret wins over a substring of it. */
export function getMatchStrings(results: RiskResult[] | undefined): string[] {
  if (!results) return [];
  const set = new Set<string>();
  for (const r of results) {
    if (!matchIsMessageContent(r)) continue;
    if (r.match) set.add(r.match);
  }
  return [...set].sort((a, b) => b.length - a.length);
}

export function maskValue(value: string): string {
  // Mask character-for-character so revealing/hiding doesn't change the text
  // length (and thus doesn't shift surrounding layout).
  return "•".repeat(value.length);
}

// Keep a per-row reveal toggle in sync with the panel-wide "reveal all" switch
// without letting the global object clobber a manual per-row click.
export function useRowReveal(sensitive: boolean): {
  revealed: boolean;
  setRevealed: (next: boolean) => void;
} {
  const reveal = useRevealAll();
  const generation = reveal?.generation;
  const revealAll = reveal?.revealAll ?? false;
  const [revealed, setRevealed] = useState(sensitive ? revealAll : true);
  const lastGeneration = useRef(generation);
  const lastSensitive = useRef(sensitive);
  useEffect(() => {
    const becameSensitive = sensitive && !lastSensitive.current;
    const generationChanged =
      generation !== undefined && lastGeneration.current !== generation;
    lastSensitive.current = sensitive;
    if (generation !== undefined) lastGeneration.current = generation;
    // Re-apply the masked (reveal-all) default when the row newly becomes
    // sensitive — e.g. findings arrive after first paint — so it can't stay
    // revealed from its earlier non-sensitive state, and when the panel-wide
    // reveal-all switch flips. Manual per-row toggles between those are kept.
    if (sensitive && (becameSensitive || generationChanged)) {
      setRevealed(revealAll);
    }
  }, [sensitive, generation, revealAll]);
  return { revealed, setRevealed };
}

/** Wrap every occurrence of `matches` in `text` with a yellow highlight. When
 * `masked`, the matched characters are dotted out (the surrounding context
 * stays visible). */
export function highlightMatches(
  text: string,
  matches: string[],
  masked: boolean,
  /** The spans are maskable (sensitive), so render them with the fixed-width
   * style in both states even when currently revealed — avoids reflow on toggle. */
  maskable = false,
): ReactNode {
  if (matches.length === 0) return text;

  const ranges: Array<[number, number]> = [];
  for (const match of matches) {
    if (!match) continue;
    let from = 0;
    let idx = text.indexOf(match, from);
    while (idx !== -1) {
      ranges.push([idx, idx + match.length]);
      from = idx + match.length;
      idx = text.indexOf(match, from);
    }
  }
  if (ranges.length === 0) return text;

  ranges.sort((a, b) => a[0] - b[0]);
  const merged: Array<[number, number]> = [];
  for (const range of ranges) {
    const last = merged[merged.length - 1];
    if (last && range[0] <= last[1]) {
      last[1] = Math.max(last[1], range[1]);
    } else {
      merged.push([range[0], range[1]]);
    }
  }

  const nodes: ReactNode[] = [];
  let pos = 0;
  merged.forEach(([start, end], k) => {
    if (start > pos) nodes.push(text.slice(pos, start));
    const value = text.slice(start, end);
    const className = maskable
      ? cn(
          SENSITIVE_MARK_BASE,
          masked ? SENSITIVE_MARK_MASKED : SENSITIVE_MARK_REVEALED,
        )
      : RISK_MARK_CLASS;
    nodes.push(
      <mark key={k} className={className}>
        {masked ? maskValue(value) : value}
      </mark>,
    );
    pos = end;
  });
  if (pos < text.length) nodes.push(text.slice(pos));
  return nodes;
}

export function findingToExclusionState(
  result: RiskResult,
): ExclusionSheetState {
  let expression: string;
  if (result.match) {
    expression = serializeExclusionExpression({
      matchType: "exact",
      matchValue: result.match,
    });
  } else if (result.ruleId) {
    expression = serializeExclusionExpression({
      matchType: "rule_id",
      matchValue: result.ruleId,
    });
  } else {
    expression = serializeExclusionExpression({
      matchType: "source",
      matchValue: result.source,
    });
  }
  return {
    mode: "create",
    initialExpression: expression,
    initialScope: result.policyId ?? GLOBAL_SCOPE,
  };
}
