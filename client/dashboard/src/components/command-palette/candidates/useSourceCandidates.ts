import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

type SourceKind = "openapi" | "function" | "externalmcp";

function detailFor(kind: SourceKind): string {
  switch (kind) {
    case "openapi":
      return "Source · OpenAPI";
    case "function":
      return "Source · function";
    case "externalmcp":
      return "Source · external MCP";
  }
}

/** Sources attached to the latest deployment: OpenAPI, functions, external MCPs. */
export function useSourceCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const { data } = useLatestDeployment(undefined, undefined, { enabled });
  const deployment = data?.deployment;

  return useMemo(() => {
    if (!deployment) return [];
    const sources: Array<{ name: string; slug: string; kind: SourceKind }> = [
      ...deployment.openapiv3Assets.map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "openapi" as const,
      })),
      ...(deployment.functionsAssets ?? []).map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "function" as const,
      })),
      ...(deployment.externalMcps ?? []).map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "externalmcp" as const,
      })),
    ];
    return sources.map((source): LauncherCandidate => ({
      id: `source:${source.kind}/${source.slug}`,
      kind: "source",
      title: source.name,
      detail: detailFor(source.kind),
      keywords: ["source", source.slug, source.kind],
      verbs: ["open"],
      icon: "file-code",
      group: "Sources",
      run: () => routes.mcp.goTo(),
    }));
  }, [deployment, routes]);
}
