import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type {
  Asset,
  DeploymentFunctions,
  DeploymentSummary,
  GetObservabilityOverviewResult,
  OpenAPIv3DeploymentAsset,
} from "@gram/client/models/components";
import {
  Chart as ChartJS,
  CategoryScale,
  Legend,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip as ChartJSTooltip,
} from "chart.js";
import { Line } from "react-chartjs-2";
import { formatDistanceToNow } from "date-fns";

ChartJS.register(
  CategoryScale,
  Legend,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  ChartJSTooltip,
);

type Source = OpenAPIv3DeploymentAsset | DeploymentFunctions;

interface TelemetrySummary {
  totalCalls: number;
  totalFailures: number;
  avgLatency: number;
  errorRate: number;
}

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function OverviewRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between px-3 py-2.5">
      <Type muted small>
        {label}
      </Type>
      <div className="text-right">{children}</div>
    </div>
  );
}

export function SourceOverviewTab({
  source,
  isOpenAPI,
  underlyingAsset,
  activeDeploymentItem,
  telemetryData,
  isLoadingTelemetry,
  sourceTelemetrySummary,
}: {
  source: Source | null;
  isOpenAPI: boolean;
  underlyingAsset: Asset | null;
  activeDeploymentItem: DeploymentSummary | null;
  telemetryData: GetObservabilityOverviewResult | undefined;
  isLoadingTelemetry: boolean;
  sourceTelemetrySummary: TelemetrySummary | null;
}) {
  const routes = useRoutes();

  const lastUpdated = underlyingAsset?.updatedAt
    ? formatDistanceToNow(new Date(underlyingAsset.updatedAt), {
        addSuffix: true,
      })
    : "Unknown";

  return (
    <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
      <div className="grid grid-cols-[280px_1fr] gap-8 items-stretch">
        {/* Source Information */}
        <div className="flex flex-col">
          <Heading variant="h4" className="mb-3">
            Source Information
          </Heading>
          <div className="border rounded-lg divide-y flex-1">
            <OverviewRow label={isOpenAPI ? "API name" : "Function name"}>
              <Type className="font-medium">{source?.name || "—"}</Type>
            </OverviewRow>
            <OverviewRow label="Source ID">
              <span className="flex items-center gap-1">
                <Type className="font-mono text-sm">
                  {source?.id ? `${source.id.slice(0, 8)}…` : "—"}
                </Type>
                {source?.id && <CopyButton text={source.id} size="inline" />}
              </span>
            </OverviewRow>
            {isOpenAPI ? (
              <OverviewRow label="Format">
                <Type className="font-mono text-sm">
                  {underlyingAsset?.contentType?.includes("yaml")
                    ? "YAML"
                    : underlyingAsset?.contentType?.includes("json")
                      ? "JSON"
                      : underlyingAsset?.contentType || "—"}
                </Type>
              </OverviewRow>
            ) : (
              <OverviewRow label="Runtime">
                <Type className="text-sm">
                  {source && "runtime" in source ? String(source.runtime) : "—"}
                </Type>
              </OverviewRow>
            )}
            <OverviewRow label="File size">
              <Type className="text-sm">
                {underlyingAsset?.contentLength
                  ? formatFileSize(underlyingAsset.contentLength)
                  : "—"}
              </Type>
            </OverviewRow>
            <OverviewRow label="Created">
              <Type className="text-sm">
                {underlyingAsset?.createdAt
                  ? dateTimeFormatters.humanize(
                      new Date(underlyingAsset.createdAt),
                    )
                  : "—"}
              </Type>
            </OverviewRow>
            <OverviewRow label="Updated">
              <Type className="text-sm">{lastUpdated}</Type>
            </OverviewRow>
            <OverviewRow label="Active deployment">
              {activeDeploymentItem ? (
                <routes.deployments.deployment.Link
                  params={[activeDeploymentItem.id]}
                  className="flex items-center gap-1 hover:no-underline"
                >
                  <Type className="font-mono text-sm text-primary">
                    {activeDeploymentItem.id.slice(0, 8)}
                  </Type>
                </routes.deployments.deployment.Link>
              ) : (
                <Type className="text-sm text-muted-foreground">—</Type>
              )}
            </OverviewRow>
          </div>
        </div>

        {/* Project Activity */}
        <div className="flex flex-col">
          <div className="flex items-center justify-between mb-3">
            <div>
              <Heading variant="h4">Project Activity</Heading>
              <Type muted small>
                All tool calls across this project
              </Type>
            </div>
            <Type muted small>
              Last 7 days
            </Type>
          </div>

          {isLoadingTelemetry ? (
            <div className="rounded-lg border border-border p-6 flex-1 animate-pulse bg-muted/20" />
          ) : telemetryData?.timeSeries &&
            telemetryData.timeSeries.length > 0 ? (
            <>
              <div className="rounded-lg border border-border p-4 flex-1 flex flex-col">
                <div className="flex-1 min-h-36">
                  <ActivityChart timeSeries={telemetryData.timeSeries} />
                </div>
              </div>
              {sourceTelemetrySummary && (
                <TelemetrySummaryRow summary={sourceTelemetrySummary} />
              )}
            </>
          ) : (
            <div className="border rounded-lg p-12 text-center flex-1 flex flex-col items-center justify-center">
              <Type muted className="block mb-1">
                No invocation data yet
              </Type>
              <Type muted small>
                Telemetry will appear here once tools from this source are
                called via an MCP server.
              </Type>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ActivityChart({
  timeSeries,
}: {
  timeSeries: NonNullable<GetObservabilityOverviewResult["timeSeries"]>;
}) {
  return (
    <Line
      data={{
        labels: timeSeries.map((b) => {
          const ts = Number(b.bucketTimeUnixNano) / 1_000_000;
          return new Date(ts).toLocaleDateString(undefined, {
            month: "short",
            day: "numeric",
          });
        }),
        datasets: [
          {
            label: "Tool Calls",
            data: timeSeries.map((b) => b.totalToolCalls),
            borderColor: "#3b82f6",
            backgroundColor: "rgba(59, 130, 246, 0.1)",
            fill: true,
            tension: 0.4,
            borderWidth: 1.5,
            pointRadius: 0,
            pointHoverRadius: 3,
          },
          {
            label: "Errors",
            data: timeSeries.map((b) => b.failedToolCalls),
            borderColor: "#ef4444",
            backgroundColor: "rgba(239, 68, 68, 0.08)",
            fill: true,
            tension: 0.4,
            borderWidth: 1.5,
            pointRadius: 0,
            pointHoverRadius: 3,
          },
        ],
      }}
      options={{
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: "index", intersect: false },
        plugins: {
          legend: {
            display: true,
            position: "top",
            align: "end",
            labels: {
              boxWidth: 8,
              boxHeight: 8,
              usePointStyle: true,
              pointStyle: "circle",
              font: { size: 11 },
            },
          },
          tooltip: {
            backgroundColor: "rgba(0,0,0,0.85)",
            titleColor: "#fff",
            bodyColor: "#e5e7eb",
            padding: 8,
            cornerRadius: 6,
          },
        },
        scales: {
          x: {
            grid: { display: false },
            ticks: { font: { size: 10 }, maxRotation: 0, maxTicksLimit: 7 },
          },
          y: {
            beginAtZero: true,
            grid: { color: "rgba(128,128,128,0.1)" },
            ticks: { font: { size: 10 } },
          },
        },
      }}
    />
  );
}

function TelemetrySummaryRow({
  summary,
}: {
  summary: {
    totalCalls: number;
    totalFailures: number;
    avgLatency: number;
    errorRate: number;
  };
}) {
  return (
    <div className="flex items-center gap-4 text-sm px-1">
      <Type muted small>
        {summary.totalCalls.toLocaleString()} calls from this source
      </Type>
      {summary.totalFailures > 0 && (
        <Type small className="text-destructive">
          {summary.totalFailures} failed
        </Type>
      )}
      <Type muted small>
        {summary.avgLatency < 1000
          ? `${summary.avgLatency.toFixed(0)}ms avg`
          : `${(summary.avgLatency / 1000).toFixed(1)}s avg`}
      </Type>
      {summary.errorRate > 0 && (
        <Type
          small
          className={
            summary.errorRate > 5 ? "text-destructive" : "text-warning"
          }
        >
          {summary.errorRate.toFixed(1)}% error rate
        </Type>
      )}
    </div>
  );
}
