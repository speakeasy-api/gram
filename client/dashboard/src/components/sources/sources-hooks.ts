import { useToolset } from "@/hooks/toolTypes";
import { useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import {
  PulseMcpAuthType,
  extractAuthType,
  isPulseMcpServer,
} from "@/pages/catalog/hooks/serverMetadata";
import { useLatestDeployment } from "@gram/client/react-query/index.js";
import { useMemo } from "react";

export function useDeploymentIsEmpty() {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  if (isLoading) {
    return false;
  }

  return (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment.packages.length === 0 &&
      (deployment.externalMcps?.length ?? 0) === 0)
  );
}

export const useCatalogIconMap = () => {
  const { data: catalogData } = useInfiniteListMCPCatalog();
  return useMemo(() => {
    if (!catalogData?.pages) {
      return new Map<string, string>();
    }
    return new Map(
      catalogData.pages.flatMap((page) =>
        page.servers.map((s) => [s.registrySpecifier, s.iconUrl!]),
      ),
    );
  }, [catalogData]);
};

export const useCatalogAuthMap = (): Map<string, PulseMcpAuthType> => {
  const { data: catalogData } = useInfiniteListMCPCatalog();

  return useMemo(() => {
    const result = new Map<string, PulseMcpAuthType>();

    if (!catalogData?.pages) {
      return result;
    }

    for (const page of catalogData.pages) {
      for (const server of page.servers) {
        if (!isPulseMcpServer(server)) continue;
        const auth = extractAuthType(server);
        result.set(server.registrySpecifier, auth);
      }
    }
    return result;
  }, [catalogData]);
};

export const useCatalogServerAuthType = (
  registrySpecifier: string,
): PulseMcpAuthType | null => {
  const authMap = useCatalogAuthMap();
  return authMap.get(registrySpecifier) ?? null;
};

export type ExternalMcpOAuthStatus =
  | "required-unconfigured"
  | "configured"
  | "not-required"
  | "not-external-mcp";

export const useExternalMcpOAuthStatus = (
  toolsetSlug: string | undefined,
): ExternalMcpOAuthStatus => {
  const { data: toolset } = useToolset(toolsetSlug);
  const { data: deploymentResult } = useLatestDeployment();
  const serverAuthMap = useCatalogAuthMap();

  return useMemo<ExternalMcpOAuthStatus>(() => {
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

    return toolset.externalOauthServer || toolset.oauthProxyServer
      ? "configured"
      : "required-unconfigured";
  }, [toolset, deploymentResult, serverAuthMap]);
};
