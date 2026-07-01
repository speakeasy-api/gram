import { Eye, EyeOff, Lock } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { RULE_CATEGORY_META } from "./policy-data";
import { getCategoryForFinding, getRuleTitleFallback } from "./risk-utils";
import { Badge } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  RevealAllContext,
  useRevealAll,
  type RevealAllContextValue,
} from "./reveal-all-context";
import { useRBAC, type Scope } from "@/hooks/useRBAC";

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

export function MaskedMatch({
  value,
}: {
  value: string | undefined;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canReveal = hasScope(REVEAL_SCOPE);
  const ctx = useRevealAll();
  const generation = ctx?.generation;
  const revealAll = ctx?.revealAll ?? false;
  const [revealed, setRevealed] = useState(revealAll);
  // Only sync when the global toggle actually fires (generation changes).
  // Depending on the context object would clobber per-row clicks on every
  // render.
  const lastSyncedGeneration = useRef(generation);
  useEffect(() => {
    if (generation === undefined) return;
    if (lastSyncedGeneration.current === generation) return;
    lastSyncedGeneration.current = generation;
    setRevealed(revealAll);
  }, [generation, revealAll]);

  if (!value) return <span>-</span>;

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

  if (!revealed) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
        onClick={(e) => {
          e.stopPropagation();
          setRevealed(true);
        }}
      >
        <EyeOff className="h-3 w-3" />
        <span>Click to reveal</span>
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
