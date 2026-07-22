import { ChartCard } from "@/components/chart/ChartCard";
import { CHART_COLORS } from "@/components/billing/breakdown-options";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { ErrorAlert } from "@/components/ui/alert";
import { Skeleton, SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import { useRoutes } from "@/routes";
import type { SkillEfficacyInsight } from "@gram/client/models/components/skillefficacyinsight.js";
import type { SkillEfficacyScoredSession } from "@gram/client/models/components/skillefficacyscoredsession.js";
import type { SkillInsightPoint } from "@gram/client/models/components/skillinsightpoint.js";
import type { SkillVersionInsight } from "@gram/client/models/components/skillversioninsight.js";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
import { useSkillEfficacyInsights } from "@gram/client/react-query/skillEfficacyInsights.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { Badge, type Column, Table } from "@speakeasy-api/moonshine";
import {
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { Line } from "react-chartjs-2";
import type { ReactNode } from "react";
import { Link } from "react-router";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
);

export const SKILL_INSIGHTS_SECTION_ID = "insights";

const METHODOLOGY_URL =
  "https://github.com/speakeasy-api/gram/blob/main/docs/skills/measuring-skill-efficacy.md";
type TrendMetric = "efficacy" | "activations" | "sessionCost";

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function formatCurrency(value: number): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: value < 1 ? 4 : 2,
  }).format(value);
}

function formatMinutes(value: number): string {
  if (value < 60) return `${value.toFixed(value < 10 ? 1 : 0)} min`;
  return `${(value / 60).toFixed(1)} hr`;
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function metricValue(point: SkillInsightPoint, metric: TrendMetric) {
  switch (metric) {
    case "efficacy":
      return point.averageScore === undefined
        ? undefined
        : point.averageScore * 100;
    case "activations":
      return point.activations;
    case "sessionCost":
      return point.sessionCostUsd;
  }
}

function formatChartValue(value: number, metric: TrendMetric): string {
  switch (metric) {
    case "efficacy":
      return `${value.toFixed(1)}%`;
    case "activations":
      return formatCount(value);
    case "sessionCost":
      return formatCurrency(value);
  }
}

export function SkillInsightsSection({
  data,
}: {
  data: GetSkillResult;
}): JSX.Element {
  const project = useProject();
  const { hasScope, isLoading: isRBACLoading, isRbacEnabled } = useRBAC();
  const canReadChats =
    !isRBACLoading && (!isRbacEnabled || hasScope("chat:read", project.id));
  const query = useSkillEfficacyInsights(
    {
      skillIds: [data.skill.id],
      includeVersions: true,
      includeScoredSessions: canReadChats,
    },
    undefined,
    { throwOnError: false, enabled: !isRBACLoading },
  );
  const versionsQuery = useSkillVersionsInfinite(
    { id: data.skill.id },
    undefined,
    { throwOnError: false },
  );
  useDrainInfiniteQuery(versionsQuery);
  const versionsLoading =
    versionsQuery.isPending ||
    versionsQuery.hasNextPage ||
    versionsQuery.isFetchingNextPage;
  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const versionLabels = new Map(
    [...versions]
      .sort(
        (left, right) => left.createdAt.getTime() - right.createdAt.getTime(),
      )
      .map((version, index) => [
        version.id,
        `v${data.skill.versionCount - versions.length + index + 1} (${version.canonicalSha256.slice(0, 8)})`,
      ]),
  );

  return (
    <SettingsSection id={SKILL_INSIGHTS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Insights</SettingsSection.Title>
        <SettingsSection.Description>
          Sampled efficacy, activation volume, attributed session cost, and
          estimated time saved over the last 30 days.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {query.error && !query.data && (
            <ErrorAlert
              title="Unable to load skill insights"
              error={query.error}
            />
          )}
          {versionsQuery.error && (
            <ErrorAlert
              title="Unable to load skill versions"
              error={versionsQuery.error}
            />
          )}
          {(query.isPending || (query.data && versionsLoading)) && (
            <InsightsLoading />
          )}
          {query.data && !versionsLoading && !versionsQuery.error && (
            <InsightsContent
              insight={query.data.insights[0]}
              scoredSessions={query.data.scoredSessions}
              canReadChats={canReadChats}
              versionLabels={versionLabels}
            />
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Efficacy and estimated savings cover sampled scored sessions.
            Session cost is attributed in full to each activated skill version,
            so totals are not additive.
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function InsightsContent({
  insight,
  scoredSessions,
  canReadChats,
  versionLabels,
}: {
  insight: SkillEfficacyInsight | undefined;
  scoredSessions: SkillEfficacyScoredSession[];
  canReadChats: boolean;
  versionLabels: Map<string, string>;
}): JSX.Element {
  if (!insight) {
    return <Type muted>No insight data is available for this skill.</Type>;
  }

  const efficacy = insight.metrics.efficacy;
  const flagRates = efficacy
    ? Object.entries(efficacy.flagCounts)
        .filter(([, count]) => count > 0)
        .map(([flag, count]) => ({
          flag: flag.replaceAll("_", " "),
          rate: count / efficacy.scoredSessions,
        }))
    : [];

  return (
    <div className="space-y-6">
      <dl className="grid gap-px overflow-hidden rounded-lg border sm:grid-cols-2 xl:grid-cols-4">
        <InsightMetric
          label="30-day activations"
          value={formatCount(insight.metrics.activations)}
          detail={`${formatCount(insight.metrics.activatedSessions)} sessions`}
        />
        <InsightMetric
          label={
            <span className="inline-flex items-center gap-2">
              Sampled efficacy
              <ReleaseStageBadge stage="beta" noTooltip />
            </span>
          }
          value={efficacy ? formatPercent(efficacy.averageScore) : "Not scored"}
          detail={
            efficacy
              ? `${formatCount(efficacy.scoredSessions)} scored sessions`
              : "Missing scores are not zero efficacy"
          }
        />
        <InsightMetric
          label="Attributed session cost"
          value={formatCurrency(insight.metrics.sessionCostUsd)}
          detail="Full session-grained cost"
        />
        <InsightMetric
          label="Estimated ROI"
          value={
            efficacy
              ? `${formatMinutes(efficacy.estimatedMinutesSavedTotal)} saved`
              : "Not estimated"
          }
          detail={
            <a
              href={METHODOLOGY_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="underline underline-offset-2"
            >
              View methodology
            </a>
          }
        />
      </dl>

      <div className="grid gap-4 xl:grid-cols-3">
        <TrendChart
          title="Efficacy trend"
          metric="efficacy"
          versions={insight.versions}
          versionLabels={versionLabels}
        />
        <TrendChart
          title="Activation volume"
          metric="activations"
          versions={insight.versions}
          versionLabels={versionLabels}
        />
        <TrendChart
          title="Attributed session cost"
          metric="sessionCost"
          versions={insight.versions}
          versionLabels={versionLabels}
        />
      </div>

      <div className="space-y-3">
        <div>
          <Type variant="subheading">Scored sessions</Type>
          <Type small muted>
            Judge rationale and raw flags for the most recent sampled sessions.
          </Type>
        </div>
        {flagRates.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {flagRates.map(({ flag, rate }) => (
              <Badge key={flag} variant="neutral">
                Marked {flag} in {formatPercent(rate)} of scored sessions
              </Badge>
            ))}
          </div>
        )}
        {!canReadChats ? (
          <Type small muted>
            The <code className="font-mono">chat:read</code> scope is required
            to view session rationale and links.
          </Type>
        ) : (
          <ScoredSessionsTable
            sessions={scoredSessions}
            versionLabels={versionLabels}
          />
        )}
      </div>
    </div>
  );
}

function InsightMetric({
  label,
  value,
  detail,
}: {
  label: ReactNode;
  value: string;
  detail: ReactNode;
}): JSX.Element {
  return (
    <div className="bg-card px-4 py-4">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="mt-1 text-2xl font-semibold tabular-nums">{value}</dd>
      <dd className="text-muted-foreground mt-1 text-xs">{detail}</dd>
    </div>
  );
}

function TrendChart({
  title,
  metric,
  versions,
  versionLabels,
}: {
  title: string;
  metric: TrendMetric;
  versions: SkillVersionInsight[];
  versionLabels: Map<string, string>;
}): JSX.Element {
  const timestamps = Array.from(
    new Set(
      versions.flatMap((version) =>
        version.trend.map((point) => point.bucketStart.getTime()),
      ),
    ),
  ).sort((left, right) => left - right);
  const labels = timestamps.map((timestamp) =>
    new Date(timestamp).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      timeZone: "UTC",
    }),
  );
  const datasets = versions.map((version, index) => {
    const points = new Map(
      version.trend.map((point) => [point.bucketStart.getTime(), point]),
    );
    const color = CHART_COLORS[index % CHART_COLORS.length];
    return {
      label: `Since ${
        versionLabels.get(version.skillVersionId) ??
        `version ${version.skillVersionId.slice(0, 8)}`
      }`,
      data: timestamps.map((timestamp) => {
        const point = points.get(timestamp);
        return point ? (metricValue(point, metric) ?? null) : null;
      }),
      borderColor: color,
      backgroundColor: color,
      pointRadius: 2,
      tension: 0.25,
      spanGaps: false,
    };
  });
  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { position: "bottom" },
      tooltip: {
        callbacks: {
          label: (context) => {
            if (context.parsed.y === null) return context.dataset.label ?? "";
            return `${context.dataset.label}: ${formatChartValue(context.parsed.y, metric)}`;
          },
        },
      },
    },
    scales: {
      y: {
        beginAtZero: true,
        max: metric === "efficacy" ? 100 : undefined,
        ticks: {
          callback: (value) => formatChartValue(Number(value), metric),
        },
      },
    },
  };

  return (
    <ChartCard
      title={title}
      chartId={`skill-${metric}`}
      expandedChart={null}
      onExpand={() => undefined}
      expandable={false}
      hasData={timestamps.length > 0}
    >
      {timestamps.length === 0 ? (
        <div className="flex h-48 items-center justify-center">
          <Type small muted>
            No trend data in this window.
          </Type>
        </div>
      ) : (
        <div className="h-56">
          <Line data={{ labels, datasets }} options={options} />
        </div>
      )}
    </ChartCard>
  );
}

function ScoredSessionsTable({
  sessions,
  versionLabels,
}: {
  sessions: SkillEfficacyScoredSession[];
  versionLabels: Map<string, string>;
}): JSX.Element {
  const routes = useRoutes();
  if (sessions.length === 0) {
    return (
      <Type small muted>
        No scored sessions in the last 30 days.
      </Type>
    );
  }
  const columns: Column<SkillEfficacyScoredSession>[] = [
    {
      key: "score",
      header: "Score",
      width: "90px",
      render: (session) => (
        <Type className="font-medium tabular-nums">
          {formatPercent(session.score)}
        </Type>
      ),
    },
    {
      key: "version",
      header: "Version",
      width: "150px",
      render: (session) => (
        <Type small mono>
          {versionLabels.get(session.skillVersionId) ??
            session.skillVersionId.slice(0, 8)}
        </Type>
      ),
    },
    {
      key: "rationale",
      header: "Rationale",
      width: "2fr",
      render: (session) => (
        <div className="space-y-1">
          <Type small>{session.rationale}</Type>
          {session.flags.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {session.flags.map((flag) => (
                <Badge key={flag} variant="neutral">
                  {flag.replaceAll("_", " ")}
                </Badge>
              ))}
            </div>
          )}
        </div>
      ),
    },
    {
      key: "activated",
      header: "Activated",
      width: "130px",
      render: (session) => (
        <Type
          small
          muted
          title={dateTimeFormatters.full.format(session.activatedAt)}
        >
          <HumanizeDateTime date={session.activatedAt} />
        </Type>
      ),
    },
    {
      key: "session",
      header: "Session",
      width: "100px",
      render: (session) =>
        session.gramChatId ? (
          <Link
            to={routes.chat.conversation.href(session.gramChatId)}
            className="text-primary text-sm underline underline-offset-2"
          >
            Open
          </Link>
        ) : (
          <Type small muted>
            Dev
          </Type>
        ),
    },
  ];

  return (
    <div className="overflow-x-auto">
      <Table
        columns={columns}
        data={sessions}
        rowKey={(session) => session.id}
        className="min-w-[800px]"
      />
    </div>
  );
}

function InsightsLoading(): JSX.Element {
  return (
    <div className="space-y-4" aria-label="Loading skill insights">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-28 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-64 rounded-lg" />
      <SkeletonTable />
    </div>
  );
}
