import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type {
  Asset,
  DeploymentFunctions,
  DeploymentSummary,
  OpenAPIv3DeploymentAsset,
  ToolMetric,
} from "@gram/client/models/components";
import { formatDistanceToNow } from "date-fns";

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
  sourceToolMetrics,
  isLoadingTelemetry,
  sourceTelemetrySummary,
}: {
  source: Source | null;
  isOpenAPI: boolean;
  underlyingAsset: Asset | null;
  activeDeploymentItem: DeploymentSummary | null;
  sourceToolMetrics: ToolMetric[];
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
      <div className="grid grid-cols-[280px_1fr] gap-8 items-start">
        {/* Source Information */}
        <div className="flex flex-col">
          <Heading variant="h4" className="mb-3">
            Source Information
          </Heading>
          <div className="border rounded-lg divide-y">
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

        {/* Source Activity */}
        <div className="flex flex-col">
          <div className="flex items-center justify-between mb-3">
            <Heading variant="h4">Source Activity</Heading>
            <Type muted small>
              Last 7 days
            </Type>
          </div>

          {isLoadingTelemetry ? (
            <div className="rounded-lg border p-6 animate-pulse bg-muted/20 h-48" />
          ) : sourceToolMetrics.length > 0 ? (
            <div className="space-y-4">
              {sourceTelemetrySummary && (
                <TelemetrySummaryRow summary={sourceTelemetrySummary} />
              )}
              <div className="border rounded-lg p-4">
                <Type muted small className="mb-3 block">
                  Tool usage
                </Type>
                <ToolBarList tools={sourceToolMetrics} />
              </div>
            </div>
          ) : (
            <div className="border rounded-lg p-12 text-center flex flex-col items-center justify-center">
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

function TelemetrySummaryRow({ summary }: { summary: TelemetrySummary }) {
  return (
    <div className="flex items-center gap-4 text-sm">
      <Type muted small>
        {summary.totalCalls.toLocaleString()} calls
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

// Brand-inspired muted palette (from moonshine gradient colors)
const barColors = [
  "bg-[hsl(214,69%,50%)]",
  "bg-[hsl(4,67%,52%)]",
  "bg-[hsl(108,35%,45%)]",
  "bg-[hsl(216,70%,60%)]",
  "bg-[hsl(23,80%,55%)]",
  "bg-[hsl(334,50%,45%)]",
  "bg-[hsl(68,45%,50%)]",
  "bg-[hsl(154,50%,40%)]",
  "bg-[hsl(220,60%,45%)]",
  "bg-[hsl(280,40%,50%)]",
];

function ToolBarList({ tools }: { tools: ToolMetric[] }) {
  const barListData = tools.slice(0, 10).map((tool) => ({
    name: tool.gramUrn.replace("tools:", ""),
    value: tool.callCount,
  }));

  if (barListData.length === 0) {
    return (
      <div className="text-center text-muted-foreground py-8">
        No tool data available
      </div>
    );
  }

  const maxValue = Math.max(...barListData.map((d) => d.value));

  return (
    <div className="space-y-2">
      {barListData.map((item, index) => {
        const widthPercent = maxValue > 0 ? (item.value / maxValue) * 100 : 0;

        return (
          <div key={item.name} className="flex items-center gap-2">
            <span className="text-sm font-medium text-right shrink-0 min-w-[3rem]">
              {item.value.toLocaleString()}
            </span>
            <div className="flex-1 relative h-7">
              <span className="absolute inset-y-0 left-2 flex items-center text-sm font-medium text-foreground truncate pr-2 z-0">
                {item.name}
              </span>
              <div
                className={`absolute inset-y-0 left-0 rounded ${barColors[index % barColors.length]}`}
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              />
              <div
                className="absolute inset-y-0 left-0 overflow-hidden z-10"
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              >
                <span className="absolute inset-y-0 left-2 flex items-center text-sm font-medium text-white truncate pr-2 whitespace-nowrap">
                  {item.name}
                </span>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
