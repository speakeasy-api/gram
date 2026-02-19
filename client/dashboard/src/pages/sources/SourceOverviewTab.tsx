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
} from "@gram/client/models/components";
import { formatDistanceToNow } from "date-fns";

type Source = OpenAPIv3DeploymentAsset | DeploymentFunctions;

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
}: {
  source: Source | null;
  isOpenAPI: boolean;
  underlyingAsset: Asset | null;
  activeDeploymentItem: DeploymentSummary | null;
}) {
  const routes = useRoutes();

  const lastUpdated = underlyingAsset?.updatedAt
    ? formatDistanceToNow(new Date(underlyingAsset.updatedAt), {
        addSuffix: true,
      })
    : "Unknown";

  return (
    <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
      {/* Source Information */}
      <div className="max-w-sm">
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
    </div>
  );
}
