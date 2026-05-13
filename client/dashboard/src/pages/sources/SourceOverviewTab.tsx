import { SourceActivityPanel } from "@/components/sources/SourceActivityPanel";
import { type SourceTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
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

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatMemory(mib: number) {
  if (mib < 1024) return `${mib} MiB`;
  const gib = mib / 1024;
  return Number.isInteger(gib) ? `${gib} GiB` : `${gib.toFixed(1)} GiB`;
}

// Mirror server/internal/constants/functions.go — applied at deploy time when
// the per-source value is NULL.
const DEFAULT_FUNCTION_MEMORY_MIB = 1024;
const DEFAULT_FUNCTION_SCALE = 2;

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
  sourceTelemetrySummary: SourceTelemetrySummary | null;
}) {
  const routes = useRoutes();

  const lastUpdated = underlyingAsset?.updatedAt
    ? formatDistanceToNow(new Date(underlyingAsset.updatedAt), {
        addSuffix: true,
      })
    : "Unknown";

  const functionSource =
    !isOpenAPI && source ? (source as DeploymentFunctions) : null;

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <div className="grid grid-cols-[280px_1fr] items-start gap-8">
        {/* Source Information */}
        <div className="flex flex-col">
          <Heading variant="h4" className="mb-3">
            Source Information
          </Heading>
          <SourceInfoTable>
            <SourceInfoRow label={isOpenAPI ? "API name" : "Function name"}>
              <Type className="font-medium">{source?.name || "—"}</Type>
            </SourceInfoRow>
            <SourceInfoRow label="Source ID">
              <span className="flex items-center gap-1">
                <Type className="font-mono text-sm">
                  {source?.id ? `${source.id.slice(0, 8)}…` : "—"}
                </Type>
                {source?.id && <CopyButton text={source.id} size="inline" />}
              </span>
            </SourceInfoRow>
            {isOpenAPI ? (
              <SourceInfoRow label="Format">
                <Type className="font-mono text-sm">
                  {underlyingAsset?.contentType?.includes("yaml")
                    ? "YAML"
                    : underlyingAsset?.contentType?.includes("json")
                      ? "JSON"
                      : underlyingAsset?.contentType || "—"}
                </Type>
              </SourceInfoRow>
            ) : (
              <>
                <SourceInfoRow label="Runtime">
                  <Type className="text-sm">
                    {functionSource ? functionSource.runtime : "—"}
                  </Type>
                </SourceInfoRow>
                <SourceInfoRow label="Memory">
                  <Type className="text-sm">
                    {formatMemory(
                      functionSource?.memoryMib ?? DEFAULT_FUNCTION_MEMORY_MIB,
                    )}
                    {functionSource?.memoryMib == null && (
                      <Type muted small as="span" className="ml-1">
                        (default)
                      </Type>
                    )}
                  </Type>
                </SourceInfoRow>
                <SourceInfoRow label="Instances">
                  <Type className="text-sm">
                    {functionSource?.scale ?? DEFAULT_FUNCTION_SCALE}
                    {functionSource?.scale == null && (
                      <Type muted small as="span" className="ml-1">
                        (default)
                      </Type>
                    )}
                  </Type>
                </SourceInfoRow>
              </>
            )}
            <SourceInfoRow label="File size">
              <Type className="text-sm">
                {underlyingAsset?.contentLength
                  ? formatFileSize(underlyingAsset.contentLength)
                  : "—"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Created">
              <Type className="text-sm">
                {underlyingAsset?.createdAt
                  ? dateTimeFormatters.humanize(
                      new Date(underlyingAsset.createdAt),
                    )
                  : "—"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Updated">
              <Type className="text-sm">{lastUpdated}</Type>
            </SourceInfoRow>
            <SourceInfoRow label="Active deployment">
              {activeDeploymentItem ? (
                <routes.deployments.deployment.Link
                  params={[activeDeploymentItem.id]}
                  className="flex items-center gap-1 hover:no-underline"
                >
                  <Type className="text-primary font-mono text-sm">
                    {activeDeploymentItem.id.slice(0, 8)}
                  </Type>
                </routes.deployments.deployment.Link>
              ) : (
                <Type className="text-muted-foreground text-sm">—</Type>
              )}
            </SourceInfoRow>
          </SourceInfoTable>
        </div>

        <SourceActivityPanel
          tools={sourceToolMetrics}
          summary={sourceTelemetrySummary}
          isLoading={isLoadingTelemetry}
          windowLabel="Last 7 days"
        />
      </div>
    </div>
  );
}
