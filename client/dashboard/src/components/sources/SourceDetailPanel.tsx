import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useLatestDeployment, useListTools } from "@/hooks/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import type { Tool } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
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
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoked a task later: browsers that start the download asynchronously
  // read the URL after click() returns, and pulling it out from under them
  // saves an empty file.
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
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

/** One labelled fact: label left, value right, in both surfaces. */
function SourceFact({
  label,
  isPage,
  children,
}: {
  label: string;
  isPage: boolean;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div
      className={cn(
        "flex items-baseline justify-between gap-4",
        // Banded on the page, where the rows run the full width and the eye
        // needs help carrying a label across to its value.
        isPage && "odd:bg-muted/20 px-6 py-3",
      )}
    >
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="min-w-0 truncate font-mono text-xs">{children}</dd>
    </div>
  );
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
  return (
    <div className="px-6 pt-5 pb-8">
      <SourceDetail sourceKind={sourceKind} assetId={assetId} />
    </div>
  );
}

/**
 * The body of the panel, which the source's own page renders too.
 *
 * Split out so a source reads the same whether it is met in the sheet beside
 * the create flow or at its own URL, and so only the padding differs.
 */
export function SourceDetail({
  sourceKind,
  assetId,
  variant = "panel",
}: {
  sourceKind: "openapi" | "function";
  assetId: string;
  /**
   * "panel" is the narrow column in the sheet: facts stacked label-left,
   * value-right, and a tool list that scrolls in place. "page" has room to
   * lay the same facts across and let the tools run down the page.
   */
  variant?: "panel" | "page";
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

  const isPage = variant === "page";

  const tools = (toolsResult?.tools ?? []).filter((tool: Tool) =>
    sourceKind === "openapi"
      ? tool.type === "http" && tool.openapiv3DocumentId === assetId
      : tool.type === "function" && tool.functionId === assetId,
  );

  const toolsSummary = isToolsError
    ? "Couldn't load this source's tools. A server built from it still starts with everything the source produced."
    : isLoading
      ? "Loading tools\u2026"
      : `${tools.length} tool${tools.length === 1 ? "" : "s"} generated from this source. A server built from it starts with all of them.`;

  const facts = (
    <>
      {file && (
        <>
          <SourceFact label="File" isPage={isPage}>
            {asset?.name ?? file.id}
          </SourceFact>
          <SourceFact label="Size" isPage={isPage}>
            {formatBytes(file.contentLength)}
          </SourceFact>
          <SourceFact label="Type" isPage={isPage}>
            {file.contentType}
          </SourceFact>
        </>
      )}
      {deployment?.id && (
        <SourceFact label="Active deployment" isPage={isPage}>
          {deployment.id}
        </SourceFact>
      )}
    </>
  );

  // The page gets the app's dashboard cards: a titled, bordered panel with
  // its rows divided inside it, rather than facts floating on white.
  if (isPage) {
    return (
      <div className="flex flex-col gap-6">
        {(file || deployment?.id) && (
          <Card.Dashboard title="Details" bodyClassName="p-0">
            <dl className="divide-border divide-y">{facts}</dl>
          </Card.Dashboard>
        )}

        <Card.Dashboard
          title="Tools"
          bodyClassName={tools.length === 0 ? undefined : "p-0"}
          action={
            <Text muted className="text-xs">
              {toolsSummary}
            </Text>
          }
        >
          {tools.length === 0 ? (
            <Text muted small>
              {isLoading
                ? "Loading tools\u2026"
                : "No tools yet. A server built from this source starts empty, and picks them up on the next deployment."}
            </Text>
          ) : (
            <ol className="divide-border divide-y">
              {tools.map((tool: Tool) => (
                <li
                  key={tool.toolUrn}
                  className="flex flex-col gap-1 px-6 py-3"
                >
                  <Text small className="font-mono">
                    {tool.name}
                  </Text>
                  {tool.description && (
                    <Text small muted>
                      {tool.description}
                    </Text>
                  )}
                </li>
              ))}
            </ol>
          )}
        </Card.Dashboard>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
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
            {facts}
          </dl>
        )}
        <Text small muted>
          {toolsSummary}
        </Text>
      </div>

      {tools.length > 0 && (
        <div className="flex flex-col gap-3">
          <Text className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
            Tools
          </Text>
          {/* A source's dozens of tools would push the file details off screen
              in the sheet, so the list scrolls in place here. */}
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
  variant = "chip",
}: {
  sourceKind: "openapi" | "function";
  assetId: string;
  /**
   * "chip" is the small bordered control the panel header wears beside Docs
   * and Close; "button" is the app's ordinary secondary button, for the
   * page's action row.
   */
  variant?: "chip" | "button";
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

  const label = isDownloading ? "Downloading" : "Download";

  if (variant === "button") {
    return (
      <Button
        type="button"
        variant="secondary"
        disabled={isDownloading}
        onClick={() => {
          void handleDownload();
        }}
      >
        <Button.Text>{label}</Button.Text>
        <Button.RightIcon>
          {isDownloading ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Download className="size-4" />
          )}
        </Button.RightIcon>
      </Button>
    );
  }

  return (
    <button
      type="button"
      disabled={isDownloading}
      onClick={() => {
        void handleDownload();
      }}
      className="text-muted-foreground hover:text-foreground bg-muted/40 hover:bg-muted flex items-center gap-1.5 border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-60"
    >
      {label}
      {isDownloading ? (
        <Loader2 className="size-3 animate-spin" />
      ) : (
        <Download className="size-3" />
      )}
    </button>
  );
}
