import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

type SourceKind = "openapi" | "function";

function detailFor(kind: SourceKind): string {
  switch (kind) {
    case "openapi":
      return "Source · OpenAPI";
    case "function":
      return "Source · function";
  }
}

/**
 * Sources attached to the latest deployment: OpenAPI documents and functions.
 * Each opens its source detail page, addressed by its deployment asset id (the
 * id the Sources page navigates by).
 */
export function useSourceCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Keyed by project: the SDK folds gramProject into the query key, so
  // omitting it would share one cache entry across projects.
  const { data } = useLatestDeployment({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });
  const deployment = data?.deployment;

  return useMemo(() => {
    if (!deployment) return [];

    const sources: Array<{
      id: string;
      name: string;
      slug: string;
      kind: SourceKind;
      open: () => void;
    }> = [
      ...deployment.openapiv3Assets.map((asset) => ({
        id: asset.id,
        name: asset.name,
        slug: asset.slug,
        kind: "openapi" as const,
        open: () => routes.mcp.sources.detail.goTo(asset.id),
      })),
      ...(deployment.functionsAssets ?? []).map((asset) => ({
        id: asset.id,
        name: asset.name,
        slug: asset.slug,
        kind: "function" as const,
        open: () => routes.mcp.sources.detail.goTo(asset.id),
      })),
    ];
    return sources.map((source): LauncherCandidate => ({
      id: `source:${source.kind}/${source.slug}`,
      kind: "source",
      title: source.name,
      detail: detailFor(source.kind),
      keywords: ["source", source.slug, source.kind, source.id],
      verbs: ["open"],
      icon: "file-code",
      group: "Sources",
      run: () => source.open(),
    }));
  }, [deployment, routes]);
}
