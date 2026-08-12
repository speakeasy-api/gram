import { Badge } from "@/components/ui/Badge";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { type RowSelection } from "@/hooks/useRowSelection";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { Sparkline } from "@/pages/costs/Sparkline";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import { RULE_CATEGORY_META, type RuleCategory } from "../policy-data";
import { CategoryLabel } from "../risk-ui";
import { getRuleTitleFallback, scoreToRating } from "../risk-utils";
import {
  SCORE_TEXT_COLOR,
  SEVERITY_ACCENT,
  SEVERITY_GROUP_LABEL,
  UNATTRIBUTED_GROUP_KEY,
  trendPercent,
  type SignalGroup,
  type SignalGroupMode,
  type SignalSeverity,
} from "./signals-helpers";

/**
 * Finding-count trend for a signal row: green/red arrow with the
 * window-over-window percentage, or a "new" badge when the previous window had
 * nothing to compare against.
 */
export function SignalTrend({
  findings,
  previousFindings,
}: {
  findings: number;
  previousFindings: number;
}): JSX.Element {
  const trend = trendPercent(findings, previousFindings);
  if (trend === null) {
    return (
      <Badge variant="information" size="sm">
        <Badge.Text>new</Badge.Text>
      </Badge>
    );
  }
  if (Math.round(trend) === 0) {
    return <span className="text-muted-foreground text-xs">±0%</span>;
  }
  const rising = trend > 0;
  return (
    <span
      className={cn(
        "text-xs font-medium tabular-nums",
        rising ? "text-destructive" : "text-success-foreground",
      )}
    >
      {rising ? "+" : ""}
      {Math.round(trend)}%
    </span>
  );
}

/** "1 user", "4 teams" — the reference row uses proper singulars. */
function pluralize(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? "" : "s"}`;
}

function groupLabel(group: SignalGroup, mode: SignalGroupMode): string {
  switch (mode) {
    case "severity":
      return SEVERITY_GROUP_LABEL[group.key as SignalSeverity] ?? group.key;
    case "category":
      return RULE_CATEGORY_META[group.key as RuleCategory]?.label ?? group.key;
    case "team":
    case "app":
      return group.key === UNATTRIBUTED_GROUP_KEY ? "Unattributed" : group.key;
  }
}

function SignalRow({
  signal,
  active,
  selection,
  onSelect,
}: {
  signal: RiskSignal;
  active: boolean;
  selection: RowSelection<RiskSignal>;
  onSelect: (signal: RiskSignal) => void;
}): JSX.Element {
  const rating = scoreToRating(signal.riskScore);
  const usersLine =
    signal.teams > 0
      ? `${pluralize(signal.users, "user")} · ${pluralize(signal.teams, "team")}`
      : pluralize(signal.users, "user");
  // The open action and the selection checkbox are sibling controls: nesting
  // the checkbox inside a role="button" row would flatten it out of the
  // accessibility tree (button descendants are presentational) and make it
  // unreachable for keyboard/screen-reader users.
  return (
    <div
      style={{ borderLeftColor: SEVERITY_ACCENT[rating] }}
      className={cn(
        "bg-card hover:bg-muted/40 divide-border flex w-full items-stretch divide-x border-l-2 transition-colors",
        active && "bg-muted/30",
      )}
    >
      <div
        className="flex shrink-0 cursor-pointer items-center px-3"
        onClick={(e) => {
          // The whole checkbox cell is a selection target: clicks anywhere in
          // it toggle the row. The checkbox itself already toggles via
          // onCheckedChange, so only the padding around it toggles here.
          if ((e.target as HTMLElement).closest("button")) return;
          selection.toggle(signal.key);
        }}
      >
        <Checkbox
          checked={selection.isSelected(signal.key)}
          onCheckedChange={() => selection.toggle(signal.key)}
          aria-label="Select signal"
        />
      </div>
      <div
        role="button"
        tabIndex={0}
        aria-label={`Open details for ${getRuleTitleFallback(signal.ruleId)}`}
        onClick={() => onSelect(signal)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onSelect(signal);
          }
        }}
        className="divide-border flex min-w-0 flex-1 cursor-pointer items-stretch divide-x text-left"
      >
        <div className="flex w-28 shrink-0 flex-col items-start justify-center gap-1 px-4 py-3">
          <span
            className="font-display text-4xl leading-none font-thin"
            style={{ color: SCORE_TEXT_COLOR[rating] }}
          >
            {signal.riskScore.toFixed(1)}
          </span>
          <span
            className="font-mono text-[11px] tracking-wide uppercase"
            style={{ color: SCORE_TEXT_COLOR[rating] }}
          >
            {signal.severity}
          </span>
        </div>
        <div className="flex min-w-0 flex-1 flex-col justify-center gap-1 px-4 py-3">
          <Text className="font-semibold">
            {getRuleTitleFallback(signal.ruleId)}
          </Text>
          {signal.description && (
            <Text small muted className="line-clamp-2">
              {signal.description}
            </Text>
          )}
          <div className="flex flex-wrap items-center gap-3 pt-1">
            <CategoryLabel
              source={signal.detectionSources[0]}
              ruleId={signal.ruleId}
            />
            {signal.apps.map((app) => (
              <span
                key={app}
                className="text-muted-foreground inline-flex items-center gap-1.5 font-mono text-xs"
              >
                <span aria-hidden className="bg-foreground/70 size-2" />
                {formatPlatform(app)}
              </span>
            ))}
          </div>
        </div>
        <div className="flex w-54 shrink-0 flex-col justify-center gap-1 px-4 py-3">
          <div className="flex items-baseline justify-between">
            <span className="text-lg font-normal tabular-nums">
              {signal.findings.toLocaleString()}
            </span>
            <SignalTrend
              findings={signal.findings}
              previousFindings={signal.previousFindings}
            />
          </div>
          <Sparkline values={signal.sparkline} width={184} height={28} />
          <span className="text-muted-foreground font-mono text-xs">
            {usersLine}
          </span>
        </div>
      </div>
    </div>
  );
}

/**
 * The ranked signal list, optionally sectioned by severity or category. The
 * server caps signals at 200 rows, so a plain map (no virtualizer) is fine.
 */
export function SignalsList({
  groups,
  mode,
  selectedKey,
  selection,
  onSelect,
}: {
  groups: SignalGroup[];
  mode: SignalGroupMode;
  selectedKey: string | null;
  selection: RowSelection<RiskSignal>;
  onSelect: (signal: RiskSignal) => void;
}): JSX.Element {
  return (
    <div className="space-y-6">
      {groups.map((group) => {
        const label = groupLabel(group, mode);
        return (
          <div key={group.key} className="space-y-2">
            {label && (
              <Text small muted className="font-medium uppercase">
                {label} · {group.signals.length}
              </Text>
            )}
            <div className="border-border divide-border divide-y overflow-hidden rounded-lg border">
              {group.signals.map((signal) => (
                <SignalRow
                  key={signal.key}
                  signal={signal}
                  active={signal.key === selectedKey}
                  selection={selection}
                  onSelect={onSelect}
                />
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}
