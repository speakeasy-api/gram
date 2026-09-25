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
// A toolset can carry several external MCPs, so every slug is reported.
function externalMcpSlugsOf(toolset: ToolsetRef): Set<string> {
  const slugs = new Set<string>();
  for (const urn of toolset.toolUrns ?? []) {
    const [, kind, slug] = urn.split(":");
    if (kind === "externalmcp" && slug) slugs.add(slug);
  }
  return slugs;
}

/**
 * Sources attached to the latest deployment: OpenAPI, functions, external
 * MCPs. Each opens the page the source list opens: OpenAPI documents and
 * functions have a source detail page addressed by their deployment asset id
 * (the id the Sources page navigates by), and an external MCP has no page of
 * its own, so it opens the hosted server built from it, resolved through the
 * toolset whose tool URNs carry the source's slug. An external MCP no server
 * uses yet, or that several servers carry, falls back to the sources list.
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
    const toolsetsByExternalMcpSlug = new Map<string, ToolsetRef[]>();
    for (const toolset of toolsetsData?.toolsets ?? []) {
      for (const slug of externalMcpSlugsOf(toolset)) {
        const carriers = toolsetsByExternalMcpSlug.get(slug) ?? [];
        carriers.push(toolset);
        toolsetsByExternalMcpSlug.set(slug, carriers);
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
          // Several servers can carry the same external MCP; opening one of
          // them would land on an arbitrary server, so only a single carrier
          // resolves and the list is the honest target otherwise (as
          // resolveLegacySourceRedirect reasons for the old source routes).
          const carriers = toolsetsByExternalMcpSlug.get(asset.slug) ?? [];
          const [toolset] = carriers;
          if (toolset && carriers.length === 1) {
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
