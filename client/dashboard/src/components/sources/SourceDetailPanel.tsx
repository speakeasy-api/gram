import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useLatestDeployment, useListTools } from "@/hooks/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import type { Tool } from "@/lib/toolTypes";
import { Download, Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

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

// The serve endpoints stream the raw file rather than JSON, so they sit
// outside the generated SDK: fetch them by hand and hand the blob to an
// anchor. A plain <a download> can't do it — the request needs the session
// cookie and the `gram-project` header.
async function downloadSource({
  assetId,
  projectId,
  projectSlug,
  isOpenAPI,
  filename,
}: {
  assetId: string;
  projectId: string;
  projectSlug: string | undefined;
  isOpenAPI: boolean;
  filename: string;
}): Promise<void> {
  const url = new URL(
    isOpenAPI ? "/rpc/assets.serveOpenAPIv3" : "/rpc/assets.serveFunction",
    getServerURL(),
  );
  url.searchParams.set("id", assetId);
  url.searchParams.set("project_id", projectId);

  const request = new Request(url.toString(), {
    method: "GET",
    credentials: "include",
  });
  if (projectSlug) request.headers.set("gram-project", projectSlug);

  const response = await fetch(request);
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement("a");
    anchor.href = objectUrl;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    URL.revokeObjectURL(objectUrl);
  }
}

// The file the user gets back should be named like the thing they picked, and
// carry the extension its content actually has: a function bundle is a zip,
// and an OpenAPI document is whichever of YAML/JSON was uploaded.
function downloadFilename(
  isOpenAPI: boolean,
  name: string | undefined,
  contentType: string | undefined,
): string {
  const base = (name ?? "source").replace(/\.(zip|ya?ml|json)$/i, "");
  if (!isOpenAPI) return `${base}.zip`;
  return `${base}.${contentType?.includes("json") ? "json" : "yaml"}`;
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
  const {
    data: toolsResult,
    isLoading,
    isError: isToolsError,
  } = useListTools();
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
        {(file || deployment?.id) && (
          <dl className="border-foreground/10 mt-1 flex flex-col gap-2 border-t pt-3">
            {file && (
              <>
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
              </>
            )}
            {deployment?.id && (
              <div className="flex items-baseline justify-between gap-4">
                <dt className="text-muted-foreground text-xs">
                  Active deployment
                </dt>
                <dd className="min-w-0 truncate font-mono text-xs">
                  {deployment.id}
                </dd>
              </div>
            )}
          </dl>
        )}
        <Text small muted>
          {isToolsError
            ? "Couldn't load this source's tools. The server is still created with everything the source produced."
            : isLoading
              ? "Loading tools…"
              : `${tools.length} tool${tools.length === 1 ? "" : "s"} generated from this source. Creating a server from it starts with all of them.`}
        </Text>
      </div>

      {tools.length > 0 && (
        <div className="flex flex-col gap-3">
          <Text className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
            Tools
          </Text>
          {/* A source can produce dozens of tools; the list scrolls in place
              so the file details above it stay on screen. */}
          <div className="max-h-96 overflow-y-auto">
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

      {!isLoading && !isToolsError && tools.length === 0 && (
        <Text small muted>
          This source has produced no tools yet. A server built from it starts
          empty, and picks them up on the next deployment.
        </Text>
      )}
    </div>
  );
}

/**
 * The panel header's download action, rendered by the side panel rather than
 * by the body above.
 *
 * Resolves the source from the same two queries the body uses, which the page
 * has already warmed, so it costs nothing to look the asset up a second time
 * here instead of threading it through the (deliberately serializable) panel
 * descriptor.
 */
export function SourceDownloadButton({
  sourceKind,
  assetId,
}: {
  sourceKind: "openapi" | "function";
  assetId: string;
}): React.JSX.Element | null {
  const project = useProject();
  const { projectSlug } = useSlugs();
  const [isDownloading, setIsDownloading] = useState(false);
  const { data: deploymentResult } = useLatestDeployment();
  const { data: assetsResult } = useListAssets();

  const isOpenAPI = sourceKind === "openapi";
  const deployment = deploymentResult?.deployment;
  const asset = isOpenAPI
    ? deployment?.openapiv3Assets?.find((a) => a.id === assetId)
    : deployment?.functionsAssets?.find((a) => a.id === assetId);
  const file = assetsResult?.assets?.find((a) => a.id === asset?.assetId);

  if (!asset?.assetId) return null;

  const handleDownload = async () => {
    setIsDownloading(true);
    try {
      await downloadSource({
        assetId: asset.assetId,
        projectId: project.id,
        projectSlug,
        isOpenAPI,
        filename: downloadFilename(isOpenAPI, asset.name, file?.contentType),
      });
    } catch (error) {
      toast.error(
        error instanceof Error
          ? `Couldn't download this source: ${error.message}`
          : "Couldn't download this source",
      );
    } finally {
      setIsDownloading(false);
    }
  };

  return (
    <button
      type="button"
      disabled={isDownloading}
      onClick={() => {
        void handleDownload();
      }}
      className="text-muted-foreground hover:text-foreground bg-muted/40 hover:bg-muted flex items-center gap-1.5 border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-60"
    >
      {isDownloading ? "Downloading" : "Download"}
      {isDownloading ? (
        <Loader2 className="size-3 animate-spin" />
      ) : (
        <Download className="size-3" />
      )}
    </button>
  );
}
