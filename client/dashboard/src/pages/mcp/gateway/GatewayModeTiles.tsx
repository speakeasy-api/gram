import { StatTileGroup, StatTileSkeleton } from "@/components/chart/stat-tile";
import { Icon } from "@/components/ui/Icon";
import { MetricCard } from "@/components/ui/MetricCard";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useIsDarkTheme } from "@/lib/theme";
import { GATEWAY_MODE_DESCRIPTIONS, type ModeShare } from "./gatewayActivity";
import { gatewayModeColor } from "./gatewayActivityColors";

// Side by side only with room for each tile's figure and change; narrow
// containers stack them.
const TILE_GROUP_CLASS =
  "flex-col divide-x-0 divide-y @3xl:flex-row @3xl:divide-x @3xl:divide-y-0";

function percent(share: number): string {
  return `${Math.round(share * 100)}%`;
}

// The change in share is in percentage points: a share moving from 40% to 50%
// is "+10 pts", where a relative change would read "+25%" and look like the
// share itself.
function pointChange(mode: ModeShare): string | undefined {
  if (mode.share === null || mode.previousShare === null) return undefined;
  const points = Math.round((mode.share - mode.previousShare) * 100);
  if (points === 0) return "No change";
  return `${points > 0 ? "+" : "−"}${Math.abs(points)} pts`;
}

// The counts behind the share, in the words the charts' legends use.
function describeCalls(mode: ModeShare): string {
  const parts = [
    `${mode.executionCalls.toLocaleString()} tool ${mode.executionCalls === 1 ? "call" : "calls"}`,
    `${mode.discoveryCalls.toLocaleString()} discovery`,
  ];
  if (mode.executeRuns > 0) {
    parts.push(
      `${mode.executeRuns.toLocaleString()} code ${mode.executeRuns === 1 ? "run" : "runs"}`,
    );
  }
  return parts.join(" · ");
}

/**
 * One tile per discovery mode with calls in the range: the share of its calls
 * that ran a tool, and how that moved against the period before.
 */
export function GatewayModeTiles({
  modes,
  loading,
}: {
  modes: ModeShare[];
  loading: boolean;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  if (loading) {
    return (
      <StatTileGroup className={TILE_GROUP_CLASS}>
        {Array.from({ length: 3 }).map((_, i) => (
          <StatTileSkeleton key={i} />
        ))}
      </StatTileGroup>
    );
  }
  return (
    <StatTileGroup className={TILE_GROUP_CLASS}>
      {modes.map((mode) => {
        const change = pointChange(mode);
        return (
          <MetricCard
            key={mode.mode}
            size="sm"
            className="min-w-0 flex-1"
            label={
              <span className="inline-flex items-center gap-1.5">
                <span
                  aria-hidden="true"
                  className="inline-block size-2 shrink-0"
                  style={{
                    backgroundColor: gatewayModeColor(mode.mode, isDark),
                  }}
                />
                {mode.label}
                <SimpleTooltip tooltip={GATEWAY_MODE_DESCRIPTIONS[mode.mode]}>
                  <button
                    type="button"
                    aria-label={`About ${mode.label}`}
                    className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
                  >
                    <Icon name="info" className="size-3" />
                  </button>
                </SimpleTooltip>
              </span>
            }
            value={mode.share === null ? "—" : percent(mode.share)}
            tone="neutral"
            delta={
              change ? (
                <>
                  {change}
                  <span className="text-muted"> vs previous period</span>
                </>
              ) : undefined
            }
            deltaTone="neutral"
            description={describeCalls(mode)}
          />
        );
      })}
    </StatTileGroup>
  );
}
