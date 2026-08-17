import { ChartCard } from "@/components/chart/ChartCard";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  ErrorAlert,
} from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { Markdown } from "@/elements/components/Markdown";
import { useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { SettingsSection } from "@/components/detail/settings-section";
import { useRoutes } from "@/routes";
import type { SkillEfficacyInsight } from "@gram/client/models/components/skillefficacyinsight.js";
import type { SkillEfficacyScoredSession } from "@gram/client/models/components/skillefficacyscoredsession.js";
import type { SkillEfficacyRegressionSignal } from "@gram/client/models/components/skillefficacyregressionsignal.js";
import type { SkillInsightPoint } from "@gram/client/models/components/skillinsightpoint.js";
import type { SkillVersionInsight } from "@gram/client/models/components/skillversioninsight.js";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
import { useSkillEfficacyInsights } from "@gram/client/react-query/skillEfficacyInsights.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { type Column, Table } from "@/components/ui/Table";
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
import { type ReactNode, useState } from "react";
import { Link } from "react-router";
import skillEfficacyMethodology from "../../../../../docs/skills/measuring-skill-efficacy.md?raw";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
);

const SCORED_SESSIONS_PAGE_SIZE = 20;
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
  versionLabels,
  versionsLoading,
  versionsError,
}: {
  data: GetSkillResult;
  versionLabels: Map<string, string>;
  versionsLoading: boolean;
  versionsError: Error | null;
}): JSX.Element {
  const { isLoading: isRBACLoading } = useRBAC();
  const query = useSkillEfficacyInsights(
    {
      skillIds: [data.skill.id],
      includeVersions: true,
    },
    undefined,
    { throwOnError: false, enabled: !isRBACLoading },
  );
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Insights</SettingsSection.Title>
        <SettingsSection.Description>
          Sampled efficacy, activation volume, attributed session cost, and
          estimated time saved over the last 30 days.
        </SettingsSection.Description>
      </SettingsSection.Header>
      {query.error && !query.data && (
        <ErrorAlert title="Unable to load skill insights" error={query.error} />
      )}
      {versionsError && (
        <ErrorAlert
          title="Unable to load skill versions"
          error={versionsError}
        />
      )}
      {(query.isPending || (query.data && versionsLoading)) && (
        <InsightsLoading />
      )}
      {query.data && !versionsLoading && !versionsError && (
        <InsightsContent
          insight={query.data.result.insights[0]}
          skillId={data.skill.id}
          versionLabels={versionLabels}
        />
      )}
      <Text small muted>
        Efficacy and estimated savings cover sampled scored sessions. Session
        cost is attributed in full to each activated skill version, so totals
        are not additive.
      </Text>
    </SettingsSection>
  );
}

function InsightsContent({
  insight,
  skillId,
  versionLabels,
}: {
  insight: SkillEfficacyInsight | undefined;
  skillId: string;
  versionLabels: Map<string, string>;
}): JSX.Element {
  if (!insight) {
    return <Text muted>No insight data is available for this skill.</Text>;
  }

  const efficacy = insight.metrics.efficacy;
  return (
    <div className="space-y-6">
      {insight.regressionSignal?.regression && (
        <RegressionWarning
          skillId={skillId}
          signal={insight.regressionSignal}
        />
      )}
      <dl className="grid gap-px overflow-hidden border sm:grid-cols-2 xl:grid-cols-4">
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
          detail={<MethodologyDialog />}
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
    </div>
  );
}

export function ScoredSessions({
  skillId,
  versionLabels,
}: {
  skillId: string;
  versionLabels: Map<string, string>;
}): JSX.Element {
  const project = useProject();
  const { hasScope, isLoading: isRBACLoading } = useRBAC();
  const canReadChats = !isRBACLoading && hasScope("chat:read", project.id);
  const [cursors, setCursors] = useState<Array<string | undefined>>([
    undefined,
  ]);
  const pageIndex = cursors.length - 1;
  const query = useSkillEfficacyInsights(
    {
      skillIds: [skillId],
      includeVersions: true,
      includeScoredSessions: true,
      cursor: cursors[pageIndex],
      limit: SCORED_SESSIONS_PAGE_SIZE,
    },
    undefined,
    {
      enabled: canReadChats,
      throwOnError: false,
    },
  );
  const efficacy = query.data?.result.insights[0]?.metrics.efficacy;
  const flagRates = efficacy
    ? Object.entries(efficacy.flagCounts)
        .filter(([, count]) => count > 0)
        .map(([flag, count]) => ({
          flag: flag.replaceAll("_", " "),
          rate: count / efficacy.scoredSessions,
        }))
    : [];

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Scored sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Judge rationale and raw flags for recent sampled sessions.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {flagRates.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {flagRates.map(({ flag, rate }) => (
                <Badge key={flag} variant="neutral">
                  Marked {flag} in {formatPercent(rate)} of scored sessions
                </Badge>
              ))}
            </div>
          )}
          {!canReadChats && (
            <Text small muted>
              The <code className="font-mono">chat:read</code> scope is required
              to view session rationale and links.
            </Text>
          )}
          {canReadChats && query.isPending && <SkeletonTable />}
          {canReadChats && query.error && (
            <div className="space-y-2">
              <ErrorAlert
                title="Unable to load scored sessions"
                error={query.error}
              />
              <Button
                size="sm"
                variant="secondary"
                onClick={() => void query.refetch()}
              >
                Retry
              </Button>
            </div>
          )}
          {canReadChats && query.data && (
            <ScoredSessionsTable
              sessions={query.data.result.scoredSessions}
              versionLabels={versionLabels}
            />
          )}
          {canReadChats && (pageIndex > 0 || query.data?.result.nextCursor) && (
            <div className="flex items-center justify-center gap-3 border-t pt-3">
              <Button
                size="sm"
                variant="secondary"
                disabled={pageIndex === 0 || query.isFetching}
                onClick={() => setCursors((current) => current.slice(0, -1))}
              >
                Previous
              </Button>
              <Text small muted className="tabular-nums">
                Page {pageIndex + 1}
              </Text>
              <Button
                size="sm"
                variant="secondary"
                disabled={!query.data?.result.nextCursor || query.isFetching}
                onClick={() => {
                  const nextCursor = query.data?.result.nextCursor;
                  if (!nextCursor) return;
                  setCursors((current) => [...current, nextCursor]);
                }}
              >
                Next
              </Button>
            </div>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function MethodologyDialog(): JSX.Element {
  return (
    <Dialog>
      <Dialog.Trigger asChild>
        <Button variant="tertiary" size="xs" className="h-auto p-0">
          View methodology
        </Button>
      </Dialog.Trigger>
      <Dialog.Content className="max-h-[calc(100vh-2rem)] grid-rows-[minmax(0,1fr)] sm:max-w-3xl">
        <Dialog.Title className="sr-only">
          Measuring skill efficacy
        </Dialog.Title>
        <div className="min-h-0 overflow-y-auto pr-1">
          <Markdown className="text-sm">{skillEfficacyMethodology}</Markdown>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

export function RegressionWarning({
  skillId,
  signal,
}: {
  skillId: string;
  signal: SkillEfficacyRegressionSignal;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <Alert variant="warning">
      <Icon name="circle-alert" className="h-4 w-4" />
      <AlertTitle>Current version shows an efficacy regression</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>
          Current: {formatPercent(signal.currentAverageScore)} across{" "}
          {formatCount(signal.currentScoredSessions)} scored sessions. Previous:{" "}
          {formatPercent(signal.predecessorAverageScore)} across{" "}
          {formatCount(signal.predecessorScoredSessions)} scored sessions.
        </p>
        {signal.predecessorVersionId && (
          <Button size="sm" variant="secondary" asChild>
            <Link
              to={routes.skills.detail.versions.version.href(
                skillId,
                signal.predecessorVersionId,
              )}
            >
              Review version to restore
            </Link>
          </Button>
        )}
      </AlertDescription>
    </Alert>
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
  const chartColors = useSeriesColors();
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
    const color = chartColors[index % chartColors.length];
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
          <Text small muted>
            No trend data in this window.
          </Text>
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
      <Text small muted>
        No scored sessions in the last 30 days.
      </Text>
    );
  }
  const columns: Column<SkillEfficacyScoredSession>[] = [
    {
      key: "score",
      header: "Score",
      width: "90px",
      render: (session) => (
        <Text className="font-medium tabular-nums">
          {formatPercent(session.score)}
        </Text>
      ),
    },
    {
      key: "version",
      header: "Version",
      width: "150px",
      render: (session) => (
        <Text small mono>
          {versionLabels.get(session.skillVersionId) ??
            session.skillVersionId.slice(0, 8)}
        </Text>
      ),
    },
    {
      key: "rationale",
      header: "Rationale",
      width: "2fr",
      render: (session) => (
        <div className="space-y-1">
          <Text small>{session.rationale}</Text>
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
        <Text
          small
          muted
          title={dateTimeFormatters.full.format(session.activatedAt)}
        >
          <HumanizeDateTime date={session.activatedAt} />
        </Text>
      ),
    },
    {
      key: "session",
      header: "Session",
      width: "100px",
      render: (session) =>
        session.gramChatId ? (
          <Link
            to={`${routes.agentSessions.href()}?${new URLSearchParams({ chatId: session.gramChatId })}`}
            className="text-primary text-sm underline underline-offset-2"
          >
            Open
          </Link>
        ) : (
          <Text small muted>
            Dev
          </Text>
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
          <Skeleton key={index} className="h-28" />
        ))}
      </div>
      <Skeleton className="h-64" />
      <SkeletonTable />
    </div>
  );
}
