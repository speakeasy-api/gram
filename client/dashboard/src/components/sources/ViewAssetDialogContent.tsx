import { MonacoEditor } from "@/components/monaco-editor";
import { SkeletonCode } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Button, Dialog } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { NamedAsset } from "./SourceCard";

interface ViewAssetDialogContentProps {
  asset: NamedAsset;
}

function useFetchAssetContent(
  asset: NamedAsset,
  project: { id: string },
  projectSlug: string | undefined,
) {
  return useQuery<{ content: string; language: string }>({
    queryKey: ["assetContent", asset.id, asset.type],
    queryFn: async () => {
      const path =
        asset.type === "openapi"
          ? "/rpc/assets.serveOpenAPIv3"
          : "/rpc/assets.serveFunction";
      const url = new URL(path, getServerURL());
      url.searchParams.set("id", asset.id);
      url.searchParams.set("project_id", project.id);

      const request = new Request(url.toString(), {
        method: "GET",
        credentials: "include",
      });

      if (projectSlug) {
        request.headers.set("gram-project", projectSlug);
      }

      const response = await fetch(request);

      if (!response.ok) {
        throw new Error(
          `Failed to load: ${response.status} ${response.statusText}`,
        );
      }

      if (asset.type === "openapi") {
        // OpenAPI specs are served as text (YAML/JSON)
        const text = await response.text();

        // Try to parse and format as JSON if possible
        try {
          const parsed = JSON.parse(text);
          return {
            content: JSON.stringify(parsed, null, 2),
            language: "json",
          };
        } catch {
          // If not JSON, return as-is (likely YAML)
          return { content: text, language: "yaml" };
        }
      } else {
        // Function bundles are served as zip files - extract manifest.json
        const { unzipSync, strFromU8 } = await import("fflate");
        const arrayBuffer = await response.arrayBuffer();
        const uint8Array = new Uint8Array(arrayBuffer);
        const unzipped = unzipSync(uint8Array);

        // Try to extract manifest.json (contains tool definitions)
        if (unzipped["manifest.json"]) {
          const manifestText = strFromU8(unzipped["manifest.json"]);
          const manifest = JSON.parse(manifestText);
          return {
            content: JSON.stringify(manifest, null, 2),
            language: "json",
          };
        } else if (unzipped["functions.js"]) {
          return {
            content: strFromU8(unzipped["functions.js"]),
            language: "javascript",
          };
        } else {
          throw new Error("No readable content found in bundle");
        }
      }
    },
    enabled: !!asset,
  });
}

export function ViewAssetDialogContent({ asset }: ViewAssetDialogContentProps) {
  const project = useProject();
  const { projectSlug } = useSlugs();

  const {
    data: assetContent,
    isLoading,
    error,
    refetch,
  } = useFetchAssetContent(asset, project, projectSlug);

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>
          {asset.name} -{" "}
          {asset.type === "openapi" ? "OpenAPI Specification" : "Tool Manifest"}
        </Dialog.Title>
        {asset.type !== "openapi" && (
          <Type className="text-muted-foreground text-sm mt-1">
            Shows the tool definitions extracted from the function bundle
          </Type>
        )}
      </Dialog.Header>
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <SkeletonCode lines={20} />
        ) : error ? (
          <div className="text-center py-8">
            <Type className="text-destructive">
              {error instanceof Error
                ? error.message
                : "Failed to fetch content"}
            </Type>
            <Button
              variant="secondary"
              size="sm"
              className="mt-4"
              onClick={() => refetch()}
            >
              Retry
            </Button>
          </div>
        ) : assetContent ? (
          <MonacoEditor
            value={assetContent.content}
            language={assetContent.language}
            height="calc(90vh - 120px)"
          />
        ) : (
          <Type className="text-muted-foreground text-center py-8">
            No content available
          </Type>
        )}
      </div>
    </>
  );
}
