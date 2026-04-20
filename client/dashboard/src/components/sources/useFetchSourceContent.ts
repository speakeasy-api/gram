import { useQuery } from "@tanstack/react-query";
import { getServerURL } from "@/lib/utils";

export function useFetchSourceContent(
  source: { assetId: string } | null,
  isOpenAPI: boolean,
  project: { id: string },
  projectSlug: string | undefined,
) {
  return useQuery<{ content: string; language: string }>({
    queryKey: ["sourceContent", source?.assetId, isOpenAPI],
    enabled: !!source?.assetId,
    throwOnError: false,
    retry: (failureCount, error) => {
      // Don't retry on 404s — the asset simply doesn't exist
      if (error instanceof Error && error.message.includes("404")) return false;
      return failureCount < 2;
    },
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
  });
}
