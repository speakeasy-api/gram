import {
  CatalogAuthType,
  extractAuthType,
  isCatalogServer,
} from "@/pages/catalog/hooks/serverMetadata";

import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import { useMemo } from "react";
import { useToolset } from "@/hooks/toolTypes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  externalMcpOAuthConfigStatus,
  primaryExternalMcpSlug,
  type ExternalMcpOAuthConfigStatus,
} from "./external-mcp-oauth";

export const useCatalogIconMap = (): Map<string, string> => {
  const { data: catalogData } = useListMCPCatalog();
  return useMemo(() => {
    if (!catalogData?.servers) {
      return new Map<string, string>();
    }
    return new Map(
      catalogData.servers.map((s) => [s.registrySpecifier, s.iconUrl!]),
    );
  }, [catalogData]);
};

const useCatalogAuthMap = (enabled = true): Map<string, CatalogAuthType> => {
  const { data: catalogData } = useListMCPCatalog(undefined, undefined, {
    enabled,
  });

  return useMemo(() => {
    const result = new Map<string, CatalogAuthType>();

    if (!catalogData?.servers) {
      return result;
    }

    for (const server of catalogData.servers) {
      if (!isCatalogServer(server)) continue;
      const auth = extractAuthType(server);
      result.set(server.registrySpecifier, auth);
    }
    return result;
  }, [catalogData]);
};

export type { ExternalMcpOAuthConfigStatus } from "./external-mcp-oauth";

export const useExternalMcpOAuthConfigStatus = (
  toolsetSlug: string | undefined,
): ExternalMcpOAuthConfigStatus => {
  const { data: toolset } = useToolset(toolsetSlug);
  const { data: deploymentResult } = useLatestDeployment();
  // Cheap check against data we already have (no extra fetch) — only pay
  // for the catalog fetch below when this toolset could actually need it.
  const slug = useMemo(
    () => primaryExternalMcpSlug(toolset?.toolUrns),
    [toolset?.toolUrns],
  );
  const serverAuthMap = useCatalogAuthMap(!!slug);

  return useMemo<ExternalMcpOAuthConfigStatus>(() => {
    if (!toolset) return "not-external-mcp";
    return externalMcpOAuthConfigStatus({
      externalMcpSlug: slug,
      externalMcps: deploymentResult?.deployment?.externalMcps ?? [],
      catalogAuthBySpecifier: serverAuthMap,
      hasOAuthServer: !!toolset.externalOauthServer,
      hasSessionIssuer: !!toolset.userSessionIssuerSlug,
    });
  }, [toolset, slug, deploymentResult, serverAuthMap]);
};

/** The listing's per-row form of useExternalMcpOAuthConfigStatus, read from the server's toolset summary. */
export const useHostedMcpOAuthConfigStatus = (
  server: Pick<McpServer, "toolset" | "userSessionIssuerId">,
): ExternalMcpOAuthConfigStatus => {
  const { toolset, userSessionIssuerId } = server;
  const slug = toolset?.externalMcpSlugs[0];
  const { data: deploymentResult } = useLatestDeployment(undefined, undefined, {
    enabled: !!slug,
  });
  const serverAuthMap = useCatalogAuthMap(!!slug);

  return useMemo<ExternalMcpOAuthConfigStatus>(() => {
    if (!toolset) return "not-external-mcp";
    return externalMcpOAuthConfigStatus({
      externalMcpSlug: slug,
      externalMcps: deploymentResult?.deployment?.externalMcps ?? [],
      catalogAuthBySpecifier: serverAuthMap,
      hasOAuthServer: !!toolset.externalOauthServerId,
      hasSessionIssuer: !!userSessionIssuerId,
    });
  }, [toolset, userSessionIssuerId, slug, deploymentResult, serverAuthMap]);
};
