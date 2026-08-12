import { SourceActivityPanel } from "@/components/sources/SourceActivityPanel";
import { type SourceTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { Asset } from "@gram/client/models/components/asset.js";
import type { DeploymentFunctions } from "@gram/client/models/components/deploymentfunctions.js";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";
import type { OpenAPIv3DeploymentAsset } from "@gram/client/models/components/openapiv3deploymentasset.js";
import type { ToolMetric } from "@gram/client/models/components/toolmetric.js";
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
}): JSX.Element {
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
              <Text className="font-medium">{source?.name || "—"}</Text>
            </SourceInfoRow>
            <SourceInfoRow label="Source ID">
              <span className="flex items-center gap-1">
                <Text className="font-mono text-sm">
                  {source?.id ? `${source.id.slice(0, 8)}…` : "—"}
                </Text>
                {source?.id && <CopyButton text={source.id} size="xs" />}
              </span>
            </SourceInfoRow>
            {isOpenAPI ? (
              <SourceInfoRow label="Format">
                <Text className="font-mono text-sm">
                  {underlyingAsset?.contentType?.includes("yaml")
                    ? "YAML"
                    : underlyingAsset?.contentType?.includes("json")
                      ? "JSON"
                      : underlyingAsset?.contentType || "—"}
                </Text>
              </SourceInfoRow>
            ) : (
              <>
                <SourceInfoRow label="Runtime">
                  <Text className="text-sm">
                    {functionSource ? functionSource.runtime : "—"}
                  </Text>
                </SourceInfoRow>
                <SourceInfoRow label="Memory">
                  <Text className="text-sm">
                    {formatMemory(
                      functionSource?.memoryMib ?? DEFAULT_FUNCTION_MEMORY_MIB,
                    )}
                    {functionSource?.memoryMib == null && (
                      <Text muted small as="span" className="ml-1">
                        (default)
                      </Text>
                    )}
                  </Text>
                </SourceInfoRow>
                <SourceInfoRow label="Instances">
                  <Text className="text-sm">
                    {functionSource?.scale ?? DEFAULT_FUNCTION_SCALE}
                    {functionSource?.scale == null && (
                      <Text muted small as="span" className="ml-1">
                        (default)
                      </Text>
                    )}
                  </Text>
                </SourceInfoRow>
              </>
            )}
            <SourceInfoRow label="File size">
              <Text className="text-sm">
                {underlyingAsset?.contentLength
                  ? formatFileSize(underlyingAsset.contentLength)
                  : "—"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Created">
              <Text className="text-sm">
                {underlyingAsset?.createdAt
                  ? dateTimeFormatters.humanize(
                      new Date(underlyingAsset.createdAt),
                    )
                  : "—"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Updated">
              <Text className="text-sm">{lastUpdated}</Text>
            </SourceInfoRow>
            <SourceInfoRow label="Active deployment">
              {activeDeploymentItem ? (
                <routes.deployments.deployment.Link
                  params={[activeDeploymentItem.id]}
                  className="flex items-center gap-1 hover:no-underline"
                >
                  <Text className="text-primary font-mono text-sm">
                    {activeDeploymentItem.id.slice(0, 8)}
                  </Text>
                </routes.deployments.deployment.Link>
              ) : (
                <Text className="text-muted-foreground text-sm">—</Text>
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
