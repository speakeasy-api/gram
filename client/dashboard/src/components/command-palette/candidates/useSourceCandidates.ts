import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

type SourceKind = "openapi" | "function" | "externalmcp";

/** What the toolset list carries that resolving an external MCP needs. */
type ToolsetRef = { slug: string; toolUrns?: string[] | undefined };

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

// An external MCP toolset carries `tools:externalmcp:<slug>:<tool>` URNs; the
// slug is how the deployment names the source (see pages/toolsets/ServerTab).
function externalMcpSlugOf(toolset: ToolsetRef): string | undefined {
  const urn = toolset.toolUrns?.find((candidate) =>
    candidate.includes(":externalmcp:"),
  );
  return urn?.split(":")[2] || undefined;
}

/**
 * Sources attached to the latest deployment: OpenAPI, functions, external
 * MCPs. Each opens the page the source list opens: OpenAPI documents and
 * functions have a source detail page addressed by their deployment asset id
 * (the id the Sources page navigates by), and an external MCP has no page of
 * its own, so it opens the hosted server built from it, resolved through the
 * toolset whose tool URNs carry the source's slug. An external MCP no server
 * uses yet falls back to the sources list.
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
  const { data: toolsetsData } = useListToolsets({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });
  const deployment = data?.deployment;

  return useMemo(() => {
    if (!deployment) return [];
    const toolsetByExternalMcpSlug = new Map<string, ToolsetRef>();
    for (const toolset of toolsetsData?.toolsets ?? []) {
      const slug = externalMcpSlugOf(toolset);
      if (slug && !toolsetByExternalMcpSlug.has(slug)) {
        toolsetByExternalMcpSlug.set(slug, toolset);
      }
    }

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
      ...(deployment.externalMcps ?? []).map((asset) => ({
        id: asset.id,
        name: asset.name,
        slug: asset.slug,
        kind: "externalmcp" as const,
        open: () => {
          const toolset = toolsetByExternalMcpSlug.get(asset.slug);
          if (toolset) {
            routes.mcp.details.goTo(toolset.slug);
            return;
          }
          routes.mcp.sources.goTo();
        },
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
  }, [deployment, toolsetsData, routes]);
}
