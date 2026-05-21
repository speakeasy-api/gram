import { Eye, EyeOff } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { RULE_CATEGORY_META } from "./policy-data";
import { getCategoryForFinding, getRuleTitleFallback } from "./risk-utils";
import { humanizeRuleId } from "./rule-ids";
import { Badge } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "@/components/ui/tooltip";

export function CategoryLabel({
  source,
  ruleId,
}: {
  source?: string;
  ruleId?: string;
}) {
  const category = getCategoryForFinding(source, ruleId);
  const meta = category ? RULE_CATEGORY_META[category] : null;
  const unknownSourceLabel = source ? humanizeRuleId(source) : "-";
  return (
    <span
      className="block min-w-0 truncate"
      title={
        meta
          ? `${meta.label}: ${meta.description}`
          : source
            ? `Unknown source: ${source}`
            : undefined
      }
    >
      <Badge variant="neutral" className="max-w-full">
        <Badge.Text className="min-w-0 truncate">
          {meta?.label ?? unknownSourceLabel}
        </Badge.Text>
      </Badge>
    </span>
  );
}

// Renders a rule id with a tooltip-quality fallback when the dashboard
// hasn't seen this rule before. The backend may roll out new gitleaks,
// presidio, or prompt_injection rules independently of the dashboard, so
// every snake_case id needs to display legibly without a code change.
export function RuleLabel({ ruleId }: { source?: string; ruleId?: string }) {
  const label = ruleId ? getRuleTitleFallback(ruleId) : "-";
  return (
    <span className="font-mono text-xs" title={ruleId}>
      {label}
    </span>
  );
}

type RevealAllContextValue = {
  revealAll: boolean;
  setRevealAll: (next: boolean) => void;
  // Bumps when revealAll is toggled. MaskedMatch listens to this so a global
  // toggle resets any per-row state, even when the new value matches the row's
  // current local state.
  generation: number;
};

const RevealAllContext = createContext<RevealAllContextValue | null>(null);

export function RevealAllProvider({ children }: { children: ReactNode }) {
  const [revealAll, setRevealAllState] = useState(false);
  const [generation, setGeneration] = useState(0);
  const setRevealAll = useCallback((next: boolean) => {
    setRevealAllState(next);
    setGeneration((g) => g + 1);
  }, []);
  const value = useMemo(
    () => ({ revealAll, setRevealAll, generation }),
    [revealAll, setRevealAll, generation],
  );
  return (
    <RevealAllContext.Provider value={value}>
      {children}
    </RevealAllContext.Provider>
  );
}

export function RevealAllToggle({ className }: { className?: string }) {
  const ctx = useContext(RevealAllContext);
  if (!ctx) return null;
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

export function MaskedMatch({ value }: { value: string | undefined }) {
  const ctx = useContext(RevealAllContext);
  const [revealed, setRevealed] = useState(ctx?.revealAll ?? false);
  const generation = ctx?.generation ?? 0;
  const globalRevealed = ctx?.revealAll ?? false;
  useEffect(() => {
    if (ctx) setRevealed(globalRevealed);
    // Re-sync whenever the global toggle fires, even if its value is unchanged.
  }, [ctx, globalRevealed, generation]);

  if (!value) return <span>-</span>;

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
