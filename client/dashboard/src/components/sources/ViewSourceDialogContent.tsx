import { Button, Dialog } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { MonacoEditor } from "@/components/monaco-editor";
import { SkeletonCode } from "@/components/ui/skeleton";
import { useQuery } from "@tanstack/react-query";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";

interface ViewSourceDialogContentProps {
  source: {
    name: string;
    slug: string;
    assetId: string;
  } | null;
  isOpenAPI: boolean;
}

function useFetchSourceContent(
  source: { assetId: string } | null,
  isOpenAPI: boolean,
  project: { id: string },
  projectSlug: string | undefined,
) {
  return useQuery<{ content: string; language: string }>({
    queryKey: ["sourceContent", source?.assetId, isOpenAPI],
    queryFn: async () => {
      if (!source) throw new Error("No source provided");

      const path = isOpenAPI
        ? "/rpc/assets.serveOpenAPIv3"
        : "/rpc/assets.serveFunction";
      const url = new URL(path, getServerURL());
      url.searchParams.set("id", source.assetId);
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

      if (isOpenAPI) {
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
        // Use dynamic import to reduce bundle size
        const { unzipSync, strFromU8 } = await import("fflate");
        const arrayBuffer = await response.arrayBuffer();
        const uint8Array = new Uint8Array(arrayBuffer);
        const unzipped = unzipSync(uint8Array);

        // Try to extract manifest.json (contains tool definitions)
        if (unzipped["manifest.json"]) {
          const manifestText = strFromU8(unzipped["manifest.json"]);
          const manifest = JSON.parse(manifestText);
          // Pretty-print the manifest
          return {
            content: JSON.stringify(manifest, null, 2),
            language: "json",
          };
        } else if (unzipped["functions.js"]) {
          // Fallback to functions.js if no manifest
          return {
            content: strFromU8(unzipped["functions.js"]),
            language: "javascript",
          };
        } else {
          throw new Error("No readable content found in bundle");
        }
      }
    },
    enabled: !!source,
  });
}

export function ViewSourceDialogContent({
  source,
  isOpenAPI,
}: ViewSourceDialogContentProps) {
  const project = useProject();
  const { projectSlug } = useSlugs();

  const {
    data: sourceContent,
    isLoading,
    error,
    refetch,
  } = useFetchSourceContent(source, isOpenAPI, project, projectSlug);

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>
          {source?.name} -{" "}
          {isOpenAPI ? "OpenAPI Specification" : "Tool Manifest"}
        </Dialog.Title>
        {!isOpenAPI && (
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
        ) : sourceContent ? (
          <MonacoEditor
            value={sourceContent.content}
            language={sourceContent.language}
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
