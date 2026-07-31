import { useOrganization } from "@/contexts/Auth";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
  type BilledDays,
  type BillingPeriod,
  bucketDateKey,
} from "./billing-cycles";
import {
  BREAKDOWN_TOTAL,
  breakdownValueLabel,
  isServerRollupRow,
  stackModeFor,
} from "./breakdown-options";
import { BreakdownPicker } from "./breakdown-picker";
import { type GroupSeries, TokenUsagePanel } from "./token-usage-panel";
import { tumDetailsQuery } from "./tum-queries";

// Org-wide token breakdown for one billing period: stacked daily tokens by a
// selectable dimension or by token type — one unified picker drives both.
// Everything renders from the billing details request shared with the
// details table (the server scopes it to the observed agent traffic, cache
// reads excluded), except the headline total, which prefers the billed
// per-day series the usage endpoint returns — the exact numbers on the
// usage card. No data-availability pruning of the dimension list:
// dimensions without data simply chart as "(unset)".
export function TumTokenBreakdown({
  period,
  projectNames,
  billedDays,
  onSelectRange,
}: {
  period: BillingPeriod;
  // Project id → name, for labeling the Project breakdown's UUID values.
  projectNames: Map<string, string>;
  // The billed per-day series and the cycle windows it fully describes.
  billedDays: BilledDays;
  // Bar-click drill-down: narrows the page's period to the clicked bucket.
  onSelectRange: (start: Date, end: Date) => void;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  // The picker's selection, plus the last-picked dimension so switching to
  // token type and back doesn't lose the grouping. Opens on the total view —
  // the billed series that matches the usage card exactly.
  const [breakdown, setBreakdown] = useState<string>(BREAKDOWN_TOTAL);
  const [dimension, setDimension] = useState<string>(Dimension.DivisionName);
  const stackBy = stackModeFor(breakdown);

  const scope = { client, orgId: organization.id, period };
  // Shared with the details table (same key — one request).
  const { data, isFetching } = useQuery(tumDetailsQuery(scope));

  const points = useMemo(() => data?.points ?? [], [data]);

  // The selected dimension's rows. "" rows are real observed traffic that
  // lacks the attribute — charted as "(unset)", same as the details table.
  // The server's top-N remainder row is flagged as the rollup so the chart
  // pins it to the neutral color by identity, not by label.
  const groups = useMemo<GroupSeries[]>(() => {
    const rows = data?.breakdowns.find((b) => b.key === dimension)?.rows ?? [];
    return rows.map((r, i) => ({
      label: breakdownValueLabel(dimension, r.value, projectNames),
      series: r.series,
      rollup: isServerRollupRow(rows, i) || undefined,
    }));
  }, [data, dimension, projectNames]);

  // The billed series aligned to the points grid, used only when the billed
  // data COVERS every charted day — coverage, not positivity: a sealed
  // zero-token cycle is fully known (all zeros beat late-recomputed
  // telemetry), while a day outside every covered cycle window (e.g. a
  // synthesized active cycle without history) makes the whole view fall
  // back to the details totals rather than charting misleading zeros.
  const billedSeries = useMemo(() => {
    if (points.length === 0) return null;
    const series: number[] = [];
    for (const p of points) {
      const key = bucketDateKey(p.bucketTimeUnixNano);
      // Bucket dates are UTC midnights, so the key parses back to the
      // bucket's exact start instant.
      const ms = Date.parse(key);
      const coveredDay = billedDays.covered.some(
        (r) => ms >= r.start && ms < r.end,
      );
      if (!coveredDay) return null;
      series.push(billedDays.byDate.get(key) ?? 0);
    }
    return series;
  }, [points, billedDays]);

  const breakdownPicker = (
    <BreakdownPicker
      value={breakdown}
      onChange={(value) => {
        setBreakdown(value);
        // Only actual dimensions pick a breakdown; the special modes
        // (total / token type) keep the last-picked dimension.
        if (stackModeFor(value) === "group") {
          setDimension(value);
        }
      }}
    />
  );

  return (
    <TokenUsagePanel
      points={points}
      groups={groups}
      billedSeries={billedSeries}
      stackBy={stackBy}
      breakdownPicker={breakdownPicker}
      loading={isFetching && !data}
      onSelectRange={onSelectRange}
    />
  );
}
