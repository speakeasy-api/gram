import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { useLatestDeployment, useListTools } from "@/hooks/toolTypes";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import type { Tool } from "@/lib/toolTypes";

// Sizes are shown to give a sense of scale, not for accounting, so a single
// significant decimal is enough.
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`;
}

/**
 * What one source is and what it produced, for the panel beside the
 * create-from-source flow.
 *
 * Keyed by asset id alone: the panel outlives the page that opened it, so it
 * refetches rather than being handed the source. Both queries are already warm
 * from the page, so this costs nothing in practice.
 */
export function SourceDetailPanel({
  sourceKind,
  assetId,
}: {
  sourceKind: "openapi" | "function";
  assetId: string;
}): React.JSX.Element {
  const { data: deploymentResult } = useLatestDeployment();
  const { data: toolsResult, isLoading } = useListTools();
  // The deployment names the source; the asset carries the file itself.
  const { data: assetsResult } = useListAssets();

  const deployment = deploymentResult?.deployment;
  const asset =
    sourceKind === "openapi"
      ? deployment?.openapiv3Assets?.find((a) => a.id === assetId)
      : deployment?.functionsAssets?.find((a) => a.id === assetId);

  const file = assetsResult?.assets?.find((a) => a.id === asset?.assetId);

  const tools = (toolsResult?.tools ?? []).filter((tool: Tool) =>
    sourceKind === "openapi"
      ? tool.type === "http" && tool.openapiv3DocumentId === assetId
      : tool.type === "function" && tool.functionId === assetId,
  );

  return (
    <div className="flex flex-col gap-6 px-6 pt-5 pb-8">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="neutral">
            <Badge.Text>
              {sourceKind === "openapi" ? "OpenAPI document" : "Function"}
            </Badge.Text>
          </Badge>
          {asset?.slug && (
            <Text small muted className="font-mono">
              {asset.slug}
            </Text>
          )}
        </div>
        {file && (
          <dl className="border-foreground/10 mt-1 flex flex-col gap-2 border-t pt-3">
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-muted-foreground text-xs">File</dt>
              <dd className="min-w-0 truncate font-mono text-xs">
                {asset?.name ?? file.id}
              </dd>
            </div>
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-muted-foreground text-xs">Size</dt>
              <dd className="font-mono text-xs">
                {formatBytes(file.contentLength)}
              </dd>
            </div>
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-muted-foreground text-xs">Type</dt>
              <dd className="min-w-0 truncate font-mono text-xs">
                {file.contentType}
              </dd>
            </div>
          </dl>
        )}
        <Text small muted>
          {isLoading
            ? "Loading tools…"
            : `${tools.length} tool${tools.length === 1 ? "" : "s"} generated from this source. Creating a server from it starts with all of them.`}
        </Text>
      </div>

      {tools.length > 0 && (
        <div className="flex flex-col gap-3">
          <Text className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
            Tools
          </Text>
          <div className="flex flex-col">
            {tools.map((tool: Tool) => (
              <div
                key={tool.toolUrn}
                className="border-foreground/10 flex flex-col gap-1 border-b py-3 last:border-b-0"
              >
                <Text small className="font-mono">
                  {tool.name}
                </Text>
                {tool.description && (
                  <Text small muted className="line-clamp-2">
                    {tool.description}
                  </Text>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {!isLoading && tools.length === 0 && (
        <Text small muted>
          This source has produced no tools yet. A server built from it starts
          empty, and picks them up on the next deployment.
        </Text>
      )}
    </div>
  );
}
