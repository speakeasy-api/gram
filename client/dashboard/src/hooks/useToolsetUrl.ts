import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { ToolsetEntry } from "@gram/client/models/components";
import { useGetDomain } from "@gram/client/react-query";

export function useCustomDomain() {
  const {
    data: domain,
    isLoading,
    refetch,
  } = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });

  return { domain: domain, refetch: refetch, isLoading };
}

export function useMcpUrl(
  toolset:
    | Pick<
        ToolsetEntry,
        | "slug"
        | "customDomainId"
        | "mcpSlug"
        | "defaultEnvironmentSlug"
        | "mcpIsPublic"
      >
    | undefined,
): {
  url: string | undefined;
  customServerURL: string | undefined;
  installPageUrl: string;
} {
  const { domain } = useCustomDomain();
  const project = useProject();

  if (!toolset)
    return { url: undefined, customServerURL: undefined, installPageUrl: "" };

  // Determine which server URL to use
  let customServerURL: string | undefined;
  if (domain && toolset.customDomainId && domain.id == toolset.customDomainId) {
    customServerURL = `https://${domain.domain}`;
  }

  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
  const mcpUrl = `${
    toolset.mcpSlug && customServerURL ? customServerURL : getServerURL()
  }/mcp/${urlSuffix}`;

  // Always use our URL for install page when server is private, even for
  // custom domains to ensure cookie is present
  const installPageUrl = toolset.mcpIsPublic
    ? `${mcpUrl}/install`
    : `${getServerURL()}/mcp/${urlSuffix}/install`;

  return {
    url: mcpUrl,
    customServerURL,
    installPageUrl,
  };
}

/**
 * Wraps an MCP URL through the /mcp-proxy/ endpoint so cross-origin requests
 * stay same-origin and the gram_session cookie is sent by the browser.
 */
export function mcpProxyUrl(mcpUrl: string): string {
  return `${getServerURL()}/mcp-proxy/${mcpUrl.replace(/^https?:\/\//, "")}`;
}

/**
 * Returns an MCP URL that always uses the Gram domain, ignoring any custom domain.
 * Use this for internal tools like the playground where we want consistent routing.
 */
export function useInternalMcpUrl(
  toolset:
    | Pick<ToolsetEntry, "slug" | "mcpSlug" | "defaultEnvironmentSlug">
    | undefined,
): string | undefined {
  const project = useProject();

  if (!toolset) return undefined;

  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;

  return `${getServerURL()}/mcp/${urlSuffix}`;
}
