import { StatRow, type StatRowMetric } from "@/components/stat-row";
import { Stack } from "@/components/ui/Stack";
import type { ReactNode } from "react";
import {
  TemplateFrame,
  TemplateHeader,
  type TemplateFrameProps,
  type TemplateHeaderProps,
} from "./scaffold";

/**
 * OverviewPage — a dashboard: a stat row over a grid of summary/chart cards.
 * Owns the frame, the single header (with an optional time-range control in
 * the CTA slot), and the metric row's loading swap (via StatRow). Compose the
 * body from `ChartCard`s (`@/components/chart/ChartCard`) — the shared titled
 * panel with loading/error states.
 *
 *   <OverviewPage
 *     scope="observe:read"
 *     title="Risk Overview"
 *     timeRange={<TimeRangePicker … />}
 *     metrics={[{ label: "Signals", value: n, tone: "information" }]}
 *     metricsLoading={q.isPending}
 *   >
 *     <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
 *       <ChartCard …>…</ChartCard>
 *     </div>
 *   </OverviewPage>
 */
export function OverviewPage({
  scope,
  scopeAll,
  resourceId,
  breadcrumbSubstitutions,
  title,
  description,
  stage,
  area,
  /** Time-range picker (or any control) shown in the header CTA slot. */
  timeRange,
  primaryAction,
  metrics,
  metricsLoading,
  children,
}: TemplateFrameProps &
  TemplateHeaderProps & {
    timeRange?: ReactNode;
    metrics?: StatRowMetric[];
    metricsLoading?: boolean;
    children: ReactNode;
  }): JSX.Element {
  return (
    <TemplateFrame
      scope={scope}
      scopeAll={scopeAll}
      resourceId={resourceId}
      breadcrumbSubstitutions={breadcrumbSubstitutions}
    >
      <TemplateHeader
        title={title}
        description={description}
        stage={stage}
        area={area}
        primaryAction={primaryAction ?? timeRange}
      />
      <Stack gap={6}>
        {metrics != null && metrics.length > 0 && (
          <StatRow metrics={metrics} isLoading={metricsLoading} />
        )}
        {children}
      </Stack>
    </TemplateFrame>
  );
}
