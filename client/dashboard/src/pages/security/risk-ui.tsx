import { Eye, EyeOff, Loader2, Lock } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { CodeBlock } from "@/components/code";
import { Dialog } from "@/components/ui/Dialog";
import { RULE_CATEGORY_META } from "./policy-data";
import {
  getCategoryForFinding,
  getRuleTitleFallback,
  isJudgeSource,
  SEVERITY_RATING_LABEL,
  scoreToRating,
  type SeverityRating,
} from "./risk-utils";
import { Badge } from "@/components/ui/Badge";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  RevealAllContext,
  useRevealAll,
  type RevealAllContextValue,
} from "./reveal-all-context";
import { useRBAC } from "@/hooks/useRBAC";
import {
  hasRevealableEvent,
  REVEAL_DENIED_REASON,
  REVEAL_SCOPE,
  useUnmaskedMatch,
} from "./unmask";

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
        {/* The ellipsis needs overflow:hidden, but Badge.Text's default box
            hugs the glyph ink (leading-none plus cap/alphabetic
            text-box-trim), so clipping there shaves the letters themselves.
            Disable the trim and open up the line box on this truncating
            instance so the ink never crosses the clip edge. */}
        <Badge.Text className="min-w-0 truncate leading-normal [text-box-trim:none]">
          {meta.label}
        </Badge.Text>
      </Badge>
    </span>
  );
}

// Renders a rule id with a tooltip-quality fallback when the dashboard
// hasn't seen this rule before. The backend may roll out new gitleaks,
// presidio, or prompt_injection rules independently of the dashboard, so
// every snake_case id needs to display legibly without a code change.
//
// Renders as the secondary line under a CategoryLabel, so it renders nothing
// when there is no rule worth naming: judge sources own a single rule whose
// name restates the category badge above it, and a finding with no rule id has
// nothing to show. Omitting the line is what keeps the merged cell clean; a
// placeholder would read as missing data.
export function RuleLabel({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}): JSX.Element | null {
  if (!ruleId || isJudgeSource(source)) return null;
  return (
    <span className="truncate font-mono text-xs" title={ruleId}>
      {getRuleTitleFallback(ruleId)}
    </span>
  );
}

// Severity badge for a finding or policy. The score is a policy attribute; a
// finding resolves it from its owning policy. Variant maps to the qualitative
// band so the color scales with risk. Renders nothing when the score is absent
// (e.g. a finding whose policy hasn't loaded yet).
// Moonshine's badge palette has no distinct "orange", so High and Critical both
// map to destructive — the label text / numeric score still distinguishes them.
const SEVERITY_BADGE_VARIANT: Record<
  SeverityRating,
  "success" | "warning" | "destructive"
> = {
  low: "success",
  medium: "warning",
  high: "destructive",
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
    <SimpleTooltip
      tooltip={`${SEVERITY_RATING_LABEL[rating]} severity · score ${score.toFixed(1)}`}
    >
      <Badge variant={SEVERITY_BADGE_VARIANT[rating]} className={className}>
        <Badge.Text>{SEVERITY_RATING_LABEL[rating]}</Badge.Text>
      </Badge>
    </SimpleTooltip>
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
          "border-border hover:bg-muted text-muted-foreground inline-flex h-9 items-center gap-2 border px-3 text-sm transition-colors"
        }
      >
        {revealAll ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
        <span>{revealAll ? "Hide all" : "Reveal all"}</span>
      </button>
    </SimpleTooltip>
  );
}

export function MaskedMatch({
  resultId,
  matchRedacted,
  tone = "default",
  wrap = false,
}: {
  resultId: string | undefined;
  matchRedacted: string | undefined;
  /**
   * "contrast" renders for a dark code-block backdrop (the Watchdog drawer's
   * evidence card): the masked state becomes a red redaction chip and the
   * revealed value flips to the backdrop's inverse text color.
   */
  tone?: "default" | "contrast";
  /**
   * Soft-wrap the revealed value instead of scrolling it horizontally. Use in
   * detail surfaces (drawers) where the full value should stay visible; table
   * cells keep the default single-line scroll so row heights stay stable.
   */
  wrap?: boolean;
}): JSX.Element {
  const contrast = tone === "contrast";
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

  // Without chat:read the plaintext can never be revealed — keep the
  // fingerprint on screen so a reviewer can still correlate and inspect the
  // finding before suppressing it. Reveal-all must not flip this open.
  if (!canReveal) {
    return (
      <LockedRedactedMatch
        matchRedacted={matchRedacted}
        contrast={contrast}
        wrap={wrap}
      />
    );
  }

  if (!revealed || value === null) {
    return (
      <button
        type="button"
        className={cn(
          "inline-flex items-center gap-1 text-xs disabled:opacity-60",
          contrast
            ? "bg-destructive px-2 py-0.5 font-mono tracking-wide text-white uppercase hover:bg-destructive/80"
            : "text-muted-foreground hover:text-foreground",
        )}
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
    <span
      className={cn(
        "inline-flex max-w-full min-w-0 gap-1",
        wrap ? "items-start" : "items-center",
      )}
    >
      <SimpleTooltip tooltip={value}>
        <span
          className={cn(
            "min-w-0 font-mono text-xs",
            wrap
              ? "break-all whitespace-pre-wrap"
              : "overflow-x-auto whitespace-nowrap",
            contrast && "text-background",
          )}
        >
          {value}
        </span>
      </SimpleTooltip>
      <button
        type="button"
        className={cn(
          "shrink-0",
          contrast
            ? "text-background/60 hover:text-background"
            : "text-muted-foreground hover:text-foreground",
        )}
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

function prettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

// Static fingerprint for callers who lack chat:read. The lock explains why
// the plaintext stays withheld; the fingerprint itself is the reviewable
// token the list endpoints already ship as match_redacted.
function LockedRedactedMatch({
  matchRedacted,
  contrast = false,
  wrap = false,
}: {
  matchRedacted: string;
  contrast?: boolean;
  wrap?: boolean;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={REVEAL_DENIED_REASON}>
      <span
        className={cn(
          "inline-flex max-w-full min-w-0 gap-1 text-xs",
          wrap ? "items-start" : "items-center",
          contrast ? "text-background/70" : "text-muted-foreground",
        )}
      >
        <Lock
          role="img"
          aria-label={REVEAL_DENIED_REASON}
          className="h-3 w-3 shrink-0"
        />
        <span
          className={cn(
            "min-w-0 font-mono",
            wrap
              ? "break-all whitespace-pre-wrap"
              : "overflow-x-auto whitespace-nowrap",
          )}
        >
          {matchRedacted}
        </span>
      </span>
    </SimpleTooltip>
  );
}

// A judge rationale rendered for a cell that has no reveal affordance. Clamped
// to two lines rather than one: a rationale is a sentence or two, and a
// single-line clip leaves only the first few words.
function RationaleText({ text }: { text: string }): JSX.Element {
  return (
    <span className="line-clamp-2 min-w-0 text-xs" title={text}>
      {text}
    </span>
  );
}

// EventMatchDialog is the evidence cell for llm_judge / prompt_injection
// findings, whose "match" is the entire flagged event (a JSON payload with
// tool calls), not a one-line substring. The cell shows the judge's rationale
// (`risk_results.description`) inline, and opens the payload in a scrollable
// Dialog behind the same audited, chat:read-gated reveal as MaskedMatch.
//
// The rationale itself is not gated: it's model-authored prose about the
// finding, and the chat transcript's RiskBadge already renders it
// unconditionally. Only the underlying event content needs chat:read.
export function EventMatchDialog({
  resultId,
  matchRedacted,
  rationale,
}: {
  resultId: string | undefined;
  matchRedacted: string | undefined;
  rationale: string | undefined;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canReveal = hasScope(REVEAL_SCOPE);
  const [open, setOpen] = useState(false);
  const { value, isLoading, reveal } = useUnmaskedMatch(resultId ?? "");

  const summary = rationale?.trim() ? rationale.trim() : null;

  if (!resultId || !matchRedacted || !hasRevealableEvent(matchRedacted)) {
    return summary ? <RationaleText text={summary} /> : <span>-</span>;
  }

  // Without chat:read the event payload can never be revealed, so there's no
  // trigger to render. The rationale still stands on its own; with none, the
  // redacted fingerprint is the reviewable token (same as MaskedMatch).
  if (!canReveal) {
    if (summary) {
      return (
        <span className="flex min-w-0 items-center gap-1.5">
          <SimpleTooltip tooltip={REVEAL_DENIED_REASON}>
            <Lock
              role="img"
              aria-label={REVEAL_DENIED_REASON}
              className="text-muted-foreground h-3 w-3 shrink-0"
            />
          </SimpleTooltip>
          <RationaleText text={summary} />
        </span>
      );
    }
    return <LockedRedactedMatch matchRedacted={matchRedacted} />;
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) reveal();
      }}
    >
      <Dialog.Trigger asChild>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground flex min-w-0 items-center gap-1.5 text-left"
          onClick={(e) => e.stopPropagation()}
          title={
            summary
              ? `${summary}\n\nClick to view the flagged event.`
              : undefined
          }
        >
          <EyeOff className="h-3 w-3 shrink-0" />
          {summary ? (
            <span className="text-foreground line-clamp-2 min-w-0 text-xs">
              {summary}
            </span>
          ) : (
            <span className="text-xs">Click to reveal</span>
          )}
        </button>
      </Dialog.Trigger>
      <Dialog.Content className="sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Flagged event</Dialog.Title>
          <Dialog.Description>
            The full event content that was flagged for this finding.
          </Dialog.Description>
        </Dialog.Header>
        {summary ? (
          <div className="bg-muted/40 space-y-1 border p-3">
            <div className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
              Why this was flagged
            </div>
            <p className="text-sm">{summary}</p>
          </div>
        ) : null}
        {value === null ? (
          <div className="text-muted-foreground flex items-center gap-2 py-8 text-sm">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span>{isLoading ? "Revealing…" : "No event content."}</span>
          </div>
        ) : (
          <div className="max-h-[60vh] overflow-y-auto">
            <CodeBlock language="json">{prettyJSON(value)}</CodeBlock>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
