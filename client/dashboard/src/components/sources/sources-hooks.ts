import { useToolset } from "@/hooks/toolTypes";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  PulseMcpAuthType,
  extractAuthType,
  isPulseMcpServer,
} from "@/pages/catalog/hooks/serverMetadata";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useMemo } from "react";

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

const useCatalogAuthMap = (): Map<string, PulseMcpAuthType> => {
  const { data: catalogData } = useListMCPCatalog();

  return useMemo(() => {
    const result = new Map<string, PulseMcpAuthType>();

    if (!catalogData?.servers) {
      return result;
    }

    for (const server of catalogData.servers) {
      if (!isPulseMcpServer(server)) continue;
      const auth = extractAuthType(server);
      result.set(server.registrySpecifier, auth);
    }
    return result;
  }, [catalogData]);
};

export type ExternalMcpOAuthConfigStatus =
  | "required-unconfigured"
  | "configured"
  | "not-required"
  | "not-external-mcp";

export const useExternalMcpOAuthConfigStatus = (
  toolsetSlug: string | undefined,
): ExternalMcpOAuthConfigStatus => {
  const { data: toolset } = useToolset(toolsetSlug);
  const { data: deploymentResult } = useLatestDeployment();
  const serverAuthMap = useCatalogAuthMap();

  return useMemo<ExternalMcpOAuthConfigStatus>(() => {
    if (!toolset) return "not-external-mcp";

    const externalMcpUrn = toolset.toolUrns?.find((urn) =>
      urn.includes(":externalmcp:"),
    );
    if (!externalMcpUrn) return "not-external-mcp";

    const slug = externalMcpUrn.split(":")[2];
    if (!slug) return "not-external-mcp";

    const matchingMcp = deploymentResult?.deployment?.externalMcps?.find(
      (m) => m.slug === slug,
    );
    const authType = matchingMcp?.registryServerSpecifier
      ? serverAuthMap.get(matchingMcp.registryServerSpecifier)
      : undefined;

    if (authType !== "oauth") return "not-required";

    return toolset.externalOauthServer ||
      toolset.oauthProxyServer ||
      toolset.userSessionIssuerSlug
      ? "configured"
      : "required-unconfigured";
  }, [toolset, deploymentResult, serverAuthMap]);
};
