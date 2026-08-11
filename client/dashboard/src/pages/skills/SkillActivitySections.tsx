import { ChartCard } from "@/components/chart/ChartCard";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { SettingsSection } from "@/components/detail/settings-section";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
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

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
);

export const SKILL_ADOPTION_SECTION_ID = "adoption";
export const SKILL_TIMELINE_SECTION_ID = "timeline";

const utcMonthDayFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});
const MS_PER_DAY = 24 * 60 * 60 * 1000;

function metricValue(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function machineLabel(count: number): string {
  return count === 1 ? "machine" : "machines";
}

export function SkillActivitySections({
  data,
  versionLabels,
  versionsLoading,
}: {
  data: GetSkillResult;
  versionLabels: Map<string, string>;
  versionsLoading: boolean;
}): JSX.Element {
  const chartColors = useSeriesColors();
  const { skill, adoption, drift, sightingTimeline } = data;
  const firstBucket = Date.UTC(
    adoption.windowStart.getUTCFullYear(),
    adoption.windowStart.getUTCMonth(),
    adoption.windowStart.getUTCDate(),
  );
  const lastBucket = Date.UTC(
    adoption.windowEnd.getUTCFullYear(),
    adoption.windowEnd.getUTCMonth(),
    adoption.windowEnd.getUTCDate(),
  );
  const buckets = Array.from(
    { length: Math.floor((lastBucket - firstBucket) / MS_PER_DAY) + 1 },
    (_, index) => firstBucket + index * MS_PER_DAY,
  );
  const pointsByVersion = new Map<string, Map<number, number>>();
  for (const point of sightingTimeline) {
    const versionID = point.skillVersionId ?? "";
    const points = pointsByVersion.get(versionID) ?? new Map<number, number>();
    points.set(point.bucketStart.getTime(), point.activationCount);
    pointsByVersion.set(versionID, points);
  }
  const knownVersionIDs = [...versionLabels.keys()].filter((versionID) =>
    pointsByVersion.has(versionID),
  );
  const unresolvedVersionIDs = [...pointsByVersion.keys()]
    .filter((versionID) => versionID && !versionLabels.has(versionID))
    .sort();
  const versionIDs = [
    ...knownVersionIDs,
    ...unresolvedVersionIDs,
    ...(pointsByVersion.has("") ? [""] : []),
  ];
  const datasets = versionIDs.map((versionID, index) => {
    const color = chartColors[index % chartColors.length];
    const label = versionID
      ? (versionLabels.get(versionID) ?? `Version ${versionID.slice(0, 8)}`)
      : "Unknown version";
    const points = pointsByVersion.get(versionID);
    return {
      label,
      data: buckets.map((bucket) => points?.get(bucket) ?? 0),
      borderColor: color,
      backgroundColor: color,
      pointRadius: 2,
      tension: 0.25,
    };
  });
  const chartOptions: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { position: "bottom" },
      tooltip: {
        callbacks: {
          label: (context) => {
            if (context.parsed.y === null) return context.dataset.label ?? "";
            return `${context.dataset.label}: ${metricValue(context.parsed.y)}`;
          },
        },
      },
    },
    scales: {
      y: { beginAtZero: true, ticks: { precision: 0 } },
    },
  };

  return (
    <>
      <SettingsSection id={SKILL_ADOPTION_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Adoption and drift</SettingsSection.Title>
          <SettingsSection.Description>
            Activation coverage and version convergence over the last 30 days.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <div className="space-y-3">
          <dl className="grid gap-px overflow-hidden border sm:grid-cols-2 lg:grid-cols-4">
            <Metric label="Versions" value={metricValue(skill.versionCount)} />
            <Metric
              label="Active machines"
              value={metricValue(adoption.distinctHostnames)}
            />
            <Metric
              label="30-day activations"
              value={metricValue(adoption.activationsInWindow)}
            />
            <Metric
              label="Drifted"
              value={metricValue(drift.driftedMachines)}
            />
          </dl>
          <Text small muted>
            {drift.targetState === "single" && (
              <>
                {metricValue(drift.onTargetMachines)}{" "}
                {machineLabel(drift.onTargetMachines)}{" "}
                {drift.onTargetMachines === 1 ? "is" : "are"} on the distributed
                version. {metricValue(drift.indeterminateMachines)}{" "}
                {machineLabel(drift.indeterminateMachines)}{" "}
                {drift.indeterminateMachines === 1 ? "has" : "have"} an unknown
                version.
              </>
            )}
            {drift.targetState === "not_distributed" &&
              "No plugin distribution target is configured, so drift is indeterminate."}
            {drift.targetState === "ambiguous" &&
              "Multiple plugin distribution targets are configured, so drift is indeterminate."}
          </Text>
          {(skill.firstSeenAt || skill.lastSeenAt) && (
            <Text small muted>
              {skill.firstSeenAt && (
                <>
                  First activated <HumanizeDateTime date={skill.firstSeenAt} />
                </>
              )}
              {skill.firstSeenAt && skill.lastSeenAt && " · "}
              {skill.lastSeenAt && (
                <>
                  Last activated <HumanizeDateTime date={skill.lastSeenAt} />
                </>
              )}
            </Text>
          )}
        </div>
      </SettingsSection>

      <SettingsSection id={SKILL_TIMELINE_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Activation timeline</SettingsSection.Title>
          <SettingsSection.Description>
            Daily activation volume for the rolling 30-day window.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <ChartCard
          title="Activations by version"
          chartId="skill-activation-timeline"
          expandedChart={null}
          onExpand={() => undefined}
          expandable={false}
          hasData={sightingTimeline.length > 0}
          loading={versionsLoading}
        >
          {sightingTimeline.length === 0 ? (
            <div className="flex h-56 items-center justify-center">
              <Text small muted>
                No activations captured in the last 30 days.
              </Text>
            </div>
          ) : (
            <div className="h-64">
              <Line
                data={{
                  labels: buckets.map((bucket) =>
                    utcMonthDayFormatter.format(bucket),
                  ),
                  datasets,
                }}
                options={chartOptions}
              />
            </div>
          )}
        </ChartCard>
      </SettingsSection>
    </>
  );
}

function Metric({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="bg-card px-4 py-3">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="mt-1 text-xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
