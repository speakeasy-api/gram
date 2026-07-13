import { Eye, EyeOff, Loader2, Lock } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useRiskUnmaskResultMutation } from "@gram/client/react-query/riskUnmaskResult.js";
import { RULE_CATEGORY_META } from "./policy-data";
import {
  getCategoryForFinding,
  getRuleTitleFallback,
  SEVERITY_RATING_LABEL,
  scoreToRating,
  type SeverityRating,
} from "./risk-utils";
import { Badge } from "@speakeasy-api/moonshine";
import { Badge as SeverityBadgeBase } from "@/components/ui/badge";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  RevealAllContext,
  useRevealAll,
  type RevealAllContextValue,
} from "./reveal-all-context";
import { useRBAC } from "@/hooks/useRBAC";
import type { Scope } from "@gram/client/models/components/rolegrant.js";

// Revealing a flagged secret exposes the raw value captured from agent/chat
// traffic, so it is gated behind the same `chat:read` scope that grants access
// to other members' session transcripts. hasScope short-circuits to true when
// RBAC is disabled, preserving existing behavior for non-RBAC orgs.
const REVEAL_SCOPE: Scope = "chat:read";
const REVEAL_DENIED_REASON =
  "You need the chat:read scope to reveal flagged values.";

export function CategoryLabel({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}): JSX.Element {
  const category = getCategoryForFinding(source, ruleId);
  const meta = category
    ? RULE_CATEGORY_META[category]
    : RULE_CATEGORY_META.custom;
  return (
    <span
      className="block min-w-0 truncate"
      title={`${meta.label}: ${meta.description}`}
    >
      <Badge variant="neutral" className="max-w-full">
        <Badge.Text className="min-w-0 truncate">{meta.label}</Badge.Text>
      </Badge>
    </span>
  );
}

// Renders a rule id with a tooltip-quality fallback when the dashboard
// hasn't seen this rule before. The backend may roll out new gitleaks,
// presidio, or prompt_injection rules independently of the dashboard, so
// every snake_case id needs to display legibly without a code change.
export function RuleLabel({
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}): JSX.Element {
  const label = ruleId ? getRuleTitleFallback(ruleId) : "-";
  return (
    <span className="font-mono text-xs" title={ruleId}>
      {label}
    </span>
  );
}

// Severity badge for a finding or policy. The score is a policy attribute; a
// finding resolves it from its owning policy. Variant maps to the qualitative
// band so the color scales with risk. Renders nothing when the score is absent
// (e.g. a finding whose policy hasn't loaded yet).
const SEVERITY_BADGE_VARIANT: Record<
  SeverityRating,
  "secondary" | "warning" | "urgent-warning" | "destructive"
> = {
  low: "secondary",
  medium: "warning",
  high: "urgent-warning",
  critical: "destructive",
};

export function SeverityBadge({
  score,
  className,
}: {
  score: number | undefined;
  className?: string;
}): JSX.Element | null {
  if (score == null) return null;
  const rating = scoreToRating(score);
  return (
    <SeverityBadgeBase
      variant={SEVERITY_BADGE_VARIANT[rating]}
      size="sm"
      className={className}
      tooltip={`${SEVERITY_RATING_LABEL[rating]} severity · score ${score.toFixed(1)}`}
    >
      {SEVERITY_RATING_LABEL[rating]}
    </SeverityBadgeBase>
  );
}

// Numeric severity, rendered as a color-coded pill. Used in list/table columns
// where the raw score is more useful than the qualitative label — the number
// carries the exact value while the band color (shared with SeverityBadge) makes
// severity scannable at a glance.
export function SeverityScore({
  score,
  className,
}: {
  score: number | undefined;
  className?: string;
}): JSX.Element {
  if (score == null) {
    return <span className="text-muted-foreground text-sm">-</span>;
  }
  // Rate on the rounded value we actually display, so a score sitting just below
  // a band boundary (e.g. 3.96 → shown as "4.0") never renders the number in a
  // color that disagrees with the band its displayed value falls in.
  const displayed = Math.round(score * 10) / 10;
  const rating = scoreToRating(displayed);
  return (
    <SeverityBadgeBase
      variant={SEVERITY_BADGE_VARIANT[rating]}
      size="sm"
      className={cn("tabular-nums", className)}
      tooltip={`${SEVERITY_RATING_LABEL[rating]} severity`}
    >
      {displayed.toFixed(1)}
    </SeverityBadgeBase>
  );
}

export function RevealAllProvider({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const [revealAll, setRevealAllState] = useState(false);
  const [generation, setGeneration] = useState(0);
  const setRevealAll = useCallback((next: boolean) => {
    setRevealAllState(next);
    setGeneration((g) => g + 1);
  }, []);
  const value = useMemo<RevealAllContextValue>(
    () => ({ revealAll, setRevealAll, generation }),
    [revealAll, setRevealAll, generation],
  );
  return (
    <RevealAllContext.Provider value={value}>
      {children}
    </RevealAllContext.Provider>
  );
}

export function RevealAllToggle({
  className,
}: {
  className?: string;
}): JSX.Element | null {
  const { hasScope } = useRBAC();
  const ctx = useRevealAll();
  if (!ctx) return null;
  // No point offering a reveal-all control when every value stays masked.
  if (!hasScope(REVEAL_SCOPE)) return null;
  const { revealAll, setRevealAll } = ctx;
  return (
    <SimpleTooltip
      tooltip={revealAll ? "Hide all matches" : "Reveal all matches"}
    >
      <button
        type="button"
        onClick={() => setRevealAll(!revealAll)}
        aria-pressed={revealAll}
        aria-label={revealAll ? "Hide all matches" : "Reveal all matches"}
        className={
          className ??
          "border-border hover:bg-muted text-muted-foreground inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm transition-colors"
        }
      >
        {revealAll ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
        <span>{revealAll ? "Hide all" : "Reveal all"}</span>
      </button>
    </SimpleTooltip>
  );
}

// useUnmaskedMatch backs a single MaskedMatch row: it calls risk.unmaskResult
// on reveal and caches the plaintext locally so re-toggling visibility (or a
// second "reveal all" pass) never re-fetches or re-audits an already-seen
// value. Each reveal is a real, audited server call — there is no client-side
// stand-in for the plaintext until this resolves.
function useUnmaskedMatch(resultId: string): {
  value: string | null;
  isLoading: boolean;
  reveal: () => void;
} {
  const { mutate, isPending } = useRiskUnmaskResultMutation();
  const [value, setValue] = useState<string | null>(null);
  const reveal = useCallback(() => {
    if (value !== null || isPending) return;
    mutate(
      { request: { riskIDRequestBody: { id: resultId } } },
      { onSuccess: (res) => setValue(res.match) },
    );
  }, [mutate, resultId, value, isPending]);
  return { value, isLoading: isPending, reveal };
}

export function MaskedMatch({
  resultId,
  matchRedacted,
}: {
  resultId: string | undefined;
  matchRedacted: string | undefined;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canReveal = hasScope(REVEAL_SCOPE);
  const ctx = useRevealAll();
  const generation = ctx?.generation;
  const revealAll = ctx?.revealAll ?? false;
  const [revealed, setRevealed] = useState(revealAll);
  const { value, isLoading, reveal } = useUnmaskedMatch(resultId ?? "");
  // Only sync when the global toggle actually fires (generation changes).
  // Depending on the context object would clobber per-row clicks on every
  // render. Starts at `undefined` (never equal to a real generation number)
  // rather than the current `generation`, so a row that mounts *after*
  // "reveal all" is already on (e.g. a paginated page loading more rows)
  // still runs this sync once on mount and picks up the active reveal-all
  // state, instead of staying masked until the next explicit toggle.
  const lastSyncedGeneration = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (generation === undefined) return;
    if (lastSyncedGeneration.current === generation) return;
    lastSyncedGeneration.current = generation;
    setRevealed(revealAll);
    if (revealAll) reveal();
  }, [generation, revealAll, reveal]);

  if (!resultId || !matchRedacted) return <span>-</span>;

  // Without chat:read the value can never be revealed — render a static,
  // non-interactive placeholder so reveal-all can't flip it open either.
  if (!canReveal) {
    return (
      <SimpleTooltip tooltip={REVEAL_DENIED_REASON}>
        <span className="text-muted-foreground inline-flex items-center gap-1 text-xs">
          <Lock className="h-3 w-3" />
          <span>Hidden</span>
        </span>
      </SimpleTooltip>
    );
  }

  if (!revealed || value === null) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs disabled:opacity-60"
        disabled={isLoading}
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(true);
          reveal();
        }}
      >
        {isLoading ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <EyeOff className="h-3 w-3" />
        )}
        <span>{isLoading ? "Revealing…" : "Click to reveal"}</span>
      </button>
    );
  }

  return (
    <span className="inline-flex max-w-full min-w-0 items-center gap-1">
      <SimpleTooltip tooltip={value}>
        <span className="min-w-0 overflow-x-auto font-mono text-xs whitespace-nowrap">
          {value}
        </span>
      </SimpleTooltip>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground shrink-0"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(false);
        }}
      >
        <Eye className="h-3 w-3" />
      </button>
    </span>
  );
}
