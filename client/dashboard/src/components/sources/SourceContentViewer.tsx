import MonacoEditorLazy from "@/components/monaco-editor.lazy";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { SkeletonCode } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Suspense } from "react";

type SourceContent = { content: string; language: string };

/**
 * The file behind a source, as text.
 *
 * The serve endpoints stream the raw file rather than JSON, so they sit
 * outside the generated SDK. An OpenAPI document is shown as it was pushed,
 * pretty-printed when it is JSON. A function arrives as a zip bundle, so the
 * manifest inside it — the tool definitions — is what gets shown; the bundled
 * code is the fallback for a bundle without one.
 */
function useSourceContent(
  assetId: string | undefined,
  isOpenAPI: boolean,
  projectId: string,
  projectSlug: string | undefined,
) {
  return useQuery<SourceContent>({
    queryKey: ["sourceContent", assetId, isOpenAPI],
    enabled: !!assetId,
    throwOnError: false,
    retry: (failureCount, error) => {
      // A 404 is a missing file, which a retry won't fix.
      if (error instanceof Error && error.message.includes("404")) return false;
      return failureCount < 2;
    },
    queryFn: async () => {
      if (!assetId) throw new Error("No source provided");

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
        const text = await response.text();
        try {
          return {
            content: JSON.stringify(JSON.parse(text), null, 2),
            language: "json",
          };
        } catch {
          return { content: text, language: "yaml" };
        }
      }

      // Loaded on demand: the zip reader is only needed on this one screen.
      const { unzipSync, strFromU8 } = await import("fflate");
      const unzipped = unzipSync(new Uint8Array(await response.arrayBuffer()));
      const manifest = unzipped["manifest.json"];
      if (manifest) {
        return {
          content: JSON.stringify(JSON.parse(strFromU8(manifest)), null, 2),
          language: "json",
        };
      }
      const code = unzipped["functions.js"];
      if (code) {
        return { content: strFromU8(code), language: "javascript" };
      }
      throw new Error("No readable content found in bundle");
    },
  });
}

/**
 * The source's file, read in place: the OpenAPI document itself, or the tool
 * manifest extracted from a function bundle.
 */
export function SourceContentViewer({
  sourceKind,
  assetId,
}: {
  sourceKind: "openapi" | "function";
  /** The deployment asset id, as the rest of the source page is keyed. */
  assetId: string;
}): React.JSX.Element {
  const project = useProject();
  const { projectSlug } = useSlugs();
  const { data: deploymentResult } = useLatestDeployment();
  const isOpenAPI = sourceKind === "openapi";

  // The serve endpoints take the underlying file's id, not the deployment
  // asset's, so resolve one from the other the way the download button does.
  const deployment = deploymentResult?.deployment;
  const asset = isOpenAPI
    ? deployment?.openapiv3Assets?.find((a) => a.id === assetId)
    : deployment?.functionsAssets?.find((a) => a.id === assetId);

  const { data, isLoading, error, refetch } = useSourceContent(
    asset?.assetId,
    isOpenAPI,
    project.id,
    projectSlug,
  );

  let body: React.ReactNode;
  if (isLoading || (!asset && !deployment)) {
    body = (
      <div className="p-6">
        <SkeletonCode lines={16} />
      </div>
    );
  } else if (error) {
    body = (
      <div className="flex flex-col items-center gap-4 py-8 text-center">
        <Text className="text-destructive">
          {error instanceof Error ? error.message : "Failed to fetch content"}
        </Text>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            void refetch();
          }}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  } else if (data) {
    body = (
      <Suspense
        fallback={
          <div className="p-6">
            <SkeletonCode lines={16} />
          </div>
        }
      >
        <MonacoEditorLazy
          value={data.content}
          language={data.language}
          height="min(70vh, 720px)"
          wordWrap="on"
        />
      </Suspense>
    );
  } else {
    body = (
      <Text muted small className="py-8 text-center">
        No content available
      </Text>
    );
  }

  return (
    <Card.Dashboard
      title={isOpenAPI ? "OpenAPI document" : "Function manifest"}
      tooltip={
        isOpenAPI
          ? "The document as it was pushed. Tools are generated from its operations."
          : "The tool definitions extracted from the function bundle, which is what a server built from this source starts with."
      }
      bodyClassName="p-0"
    >
      {body}
    </Card.Dashboard>
  );
}
