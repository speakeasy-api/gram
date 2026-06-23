import { Eye, EyeOff } from "lucide-react";
import { type ReactElement, type ReactNode, useMemo } from "react";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import type { RiskResult } from "@gram/client/models/components";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  getMatchStrings,
  getRiskBadgeLabel,
  highlightMatches,
  maskValue,
  resultsAreSensitive,
  shouldShowRiskRuleId,
  useRowReveal,
} from "./chatRiskHelpers";

/** A short flagged message rendered inline with the matched span(s) marked in
 * yellow, plus a reveal toggle when the match is sensitive. */
export function HighlightedMessageText({
  text,
  results,
  revealed: controlledRevealed,
}: {
  text: string;
  results: RiskResult[];
  /** When provided, the host owns the reveal state (e.g. a toggle in the
   * message meta strip) and the inline toggle is hidden. */
  revealed?: boolean;
}): ReactNode {
  const matches = useMemo(() => getMatchStrings(results), [results]);
  const sensitive = resultsAreSensitive(results);
  const internal = useRowReveal(sensitive);
  const isControlled = controlledRevealed !== undefined;
  const revealed = controlledRevealed ?? internal.revealed;
  // A finding can match content that was stripped for display (e.g. the
  // <message-context> envelope), so its span isn't in the visible text. Surface
  // each such value explicitly — per-match, so an orphan isn't hidden just
  // because a sibling match happens to appear in the text.
  const orphanMatches = matches.filter((m) => m && !text.includes(m));
  return (
    <div className="space-y-1">
      {text && (
        <div className="whitespace-pre-wrap">
          {highlightMatches(text, matches, sensitive && !revealed, sensitive)}
        </div>
      )}
      {orphanMatches.length > 0 && (
        <div className="text-muted-foreground space-y-1 text-xs">
          <span>
            Flagged value{orphanMatches.length > 1 ? "s" : ""} (not shown in
            message text):
          </span>
          <div className="flex flex-wrap gap-1">
            {orphanMatches.map((m, i) => (
              <code
                key={i}
                className="bg-destructive/10 text-destructive rounded px-1 py-0.5 font-mono break-all"
              >
                {sensitive && !revealed ? maskValue(m) : m}
              </code>
            ))}
          </div>
        </div>
      )}
      {sensitive && !isControlled && (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
          onClick={() => internal.setRevealed(!internal.revealed)}
        >
          {internal.revealed ? (
            <Eye className="size-3" />
          ) : (
            <EyeOff className="size-3" />
          )}
          {internal.revealed ? "Hide secret" : "Reveal secret"}
        </button>
      )}
    </div>
  );
}

function MaskedMatchInline({ value }: { value: string }): ReactNode {
  const { revealed, setRevealed } = useRowReveal(true);
  if (!revealed) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground mt-1 inline-flex items-center gap-1 text-xs"
        onClick={() => setRevealed(true)}
      >
        <EyeOff className="h-3 w-3" />
        <span>Click to reveal</span>
      </button>
    );
  }
  return (
    <span className="mt-1 inline-flex items-center gap-1">
      <code className="bg-destructive/10 text-destructive inline-block rounded px-1.5 py-0.5 font-mono text-xs break-all">
        {value}
      </code>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground"
        onClick={() => setRevealed(false)}
      >
        <Eye className="h-3 w-3" />
      </button>
    </span>
  );
}

/** Compact "N risks" badge with a popover listing each unique finding. */
export function RiskBadge({
  results,
  compact = false,
  trigger,
}: {
  results: RiskResult[];
  compact?: boolean;
  /** Custom popover trigger (a single element, for `PopoverTrigger asChild`)
   * — e.g. the "N risks" turn-divider label. Falls back
   * to the default destructive badge. */
  trigger?: ReactElement;
}): ReactNode {
  const unique = useMemo(() => {
    const grouped = new Map<string, { result: RiskResult; count: number }>();
    for (const r of results) {
      const key = `${r.source} ${r.ruleId ?? ""} ${r.match ?? ""}`;
      const hit = grouped.get(key);
      if (hit) hit.count++;
      else grouped.set(key, { result: r, count: 1 });
    }
    return [...grouped.values()];
  }, [results]);

  return (
    <Popover>
      <PopoverTrigger asChild>
        {trigger ?? (
          <button
            type="button"
            className="cursor-pointer"
            onClick={(e) => e.stopPropagation()}
          >
            <Badge
              variant="destructive"
              className={compact ? "px-1.5 py-0 text-[10px]" : "text-xs"}
            >
              <Icon
                name="shield-alert"
                className={`mr-1 ${compact ? "size-2.5" : "size-3"}`}
              />
              {unique.length} {unique.length === 1 ? "Risk" : "Risks"}
            </Badge>
          </button>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-[70vh] w-80 overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="space-y-3">
          <div className="text-sm font-semibold">Risk Findings</div>
          <div className="divide-border divide-y">
            {unique.map(({ result: r, count }) => (
              <div key={r.id} className="py-2 first:pt-0 last:pb-0">
                <div className="flex items-center gap-2">
                  <Badge variant="destructive" className="shrink-0 text-[10px]">
                    {getRiskBadgeLabel(r)}
                  </Badge>
                  {shouldShowRiskRuleId(r) && (
                    <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
                      {r.ruleId}
                    </span>
                  )}
                  {count > 1 && (
                    <Badge
                      variant="neutral"
                      className="ml-auto shrink-0 text-[10px]"
                    >
                      ×{count}
                    </Badge>
                  )}
                </div>
                {r.description && (
                  <p className="text-muted-foreground mt-1 text-xs">
                    {r.description}
                  </p>
                )}
                {r.match && <MaskedMatchInline value={r.match} />}
                {r.tags && r.tags.length > 0 && (
                  <div className="mt-1 flex flex-wrap gap-1">
                    {r.tags.map((tag) => (
                      <Badge
                        key={tag}
                        variant="neutral"
                        className="text-[10px]"
                      >
                        {tag}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// Subtle, low-emphasis action styled to sit in the message meta strip alongside
// the cost figure — muted by default, darkens on hover, small leading icon so it
// reads as an action rather than a label.
const META_ACTION_CLASS =
  "text-muted-foreground hover:text-foreground inline-flex shrink-0 cursor-pointer items-center gap-1 text-xs transition-colors";

/** Reveal/hide toggle for a row's masked secret, styled for the meta strip. The
 * host owns the `revealed` state so the bubble's highlighted text stays in sync.
 * Renders nothing when the row has no maskable (sensitive) finding. */
export function RevealSecretButton({
  results,
  revealed,
  onToggle,
}: {
  results: RiskResult[];
  revealed: boolean;
  onToggle: () => void;
}): ReactNode {
  if (!resultsAreSensitive(results)) return null;
  return (
    <button type="button" className={META_ACTION_CLASS} onClick={onToggle}>
      {revealed ? <Eye className="size-3" /> : <EyeOff className="size-3" />}
      {revealed ? "Hide" : "Reveal"}
    </button>
  );
}

/** Thin vertical rule between items in a message meta strip. Rendered next to a
 * sibling (never on its own) so it doesn't dangle. */
export function MetaSeparator(): ReactNode {
  return <span aria-hidden className="bg-border h-3 w-px shrink-0" />;
}
