import { type ReactNode, useEffect, useRef, useState } from "react";
import type { RiskResult } from "@gram/client/models/components";
import { ruleIdCategoryLabel } from "@/pages/security/rule-ids";
import { serializeExclusionExpression } from "@/pages/security/exclusion-expression";
import {
  type ExclusionSheetState,
  GLOBAL_SCOPE,
} from "@/pages/security/exclusion-sheet";
import { getRuleTitleFallback } from "@/pages/security/risk-utils";
import { useRevealAll } from "@/pages/security/reveal-all-context";

/** Soft warning (yellow) wash used to mark a flagged span inside message text
 * or tool output. */
export const RISK_MARK_CLASS =
  "rounded-sm bg-warning-softest px-0.5 text-foreground ring-1 ring-warning-softest";

export const SNIPPET_CONTEXT_CHARS = 48;

export function getRiskBadgeLabel(result: RiskResult): string {
  if (result.ruleId === "llm_judge") return getRuleTitleFallback(result.ruleId);
  return ruleIdCategoryLabel(result.ruleId) || result.source.toUpperCase();
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

/** Distinct, non-empty match strings to highlight, longest first so a longer
 * secret wins over a substring of it. */
export function getMatchStrings(results: RiskResult[] | undefined): string[] {
  if (!results) return [];
  const set = new Set<string>();
  for (const r of results) {
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
  useEffect(() => {
    if (!sensitive) return;
    if (generation === undefined) return;
    if (lastGeneration.current === generation) return;
    lastGeneration.current = generation;
    setRevealed(revealAll);
  }, [generation, revealAll, sensitive]);
  return { revealed, setRevealed };
}

/** Wrap every occurrence of `matches` in `text` with a yellow highlight. When
 * `masked`, the matched characters are dotted out (the surrounding context
 * stays visible). */
export function highlightMatches(
  text: string,
  matches: string[],
  masked: boolean,
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
    nodes.push(
      <mark key={k} className={RISK_MARK_CLASS}>
        {masked ? maskValue(value) : value}
      </mark>,
    );
    pos = end;
  });
  if (pos < text.length) nodes.push(text.slice(pos));
  return nodes;
}

export interface SnippetMatch {
  before: string;
  value: string;
  after: string;
  truncatedStart: boolean;
  truncatedEnd: boolean;
}

export function buildSnippets(
  content: string,
  matches: string[],
): SnippetMatch[] {
  const snippets: SnippetMatch[] = [];
  const seen = new Set<string>();
  for (const match of matches) {
    if (!match || seen.has(match)) continue;
    seen.add(match);
    const idx = content.indexOf(match);
    if (idx === -1) {
      snippets.push({
        before: "",
        value: match,
        after: "",
        truncatedStart: false,
        truncatedEnd: false,
      });
      continue;
    }
    const start = Math.max(0, idx - SNIPPET_CONTEXT_CHARS);
    const end = Math.min(
      content.length,
      idx + match.length + SNIPPET_CONTEXT_CHARS,
    );
    snippets.push({
      before: content.slice(start, idx),
      value: match,
      after: content.slice(idx + match.length, end),
      truncatedStart: start > 0,
      truncatedEnd: end < content.length,
    });
  }
  return snippets;
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
