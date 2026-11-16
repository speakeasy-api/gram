import { normalizeRemoteUrl } from "@/pages/catalog/remotes";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useUnproxiedMcpServers } from "@gram/client/react-query/unproxiedMcpServers.js";
import { useListMCPCatalog } from "./hooks";
import type { ExternalMCPServerEntry } from "@gram/client/models/components/externalmcpserverentry.js";

/** Read catalog branding live; installation never snapshots it into custom metadata. */
export function useInstalledCatalogServer(
  mcpServerId: string | undefined,
  enabled = true,
  projectSlug?: string,
): ExternalMCPServerEntry | undefined {
  const request = projectSlug ? { gramProject: projectSlug } : undefined;
  const options = { enabled: enabled && !!mcpServerId, throwOnError: false };
  const { data: servers } = useMcpServers(request, undefined, options);
  const server = servers?.mcpServers.find((s) => s.id === mcpServerId);
  const { data: remotes } = useRemoteMcpServers(request, undefined, {
    ...options,
    enabled: options.enabled && !!server?.remoteMcpServerId,
  });
  const { data: unproxied } = useUnproxiedMcpServers(request, undefined, {
    ...options,
    enabled: options.enabled && !!server?.unproxiedMcpServerId,
  });
  const url =
    remotes?.remoteMcpServers.find((s) => s.id === server?.remoteMcpServerId)
      ?.url ??
    unproxied?.unproxiedMcpServers.find(
      (s) => s.id === server?.unproxiedMcpServerId,
    )?.url;
  const specifier = server?.toolset?.registrySpecifier;
  const { data: catalog } = useListMCPCatalog(undefined, undefined, {
    projectSlug,
    enabled: options.enabled && !!(url || specifier),
    throwOnError: false,
  });
  if (!options.enabled) return undefined;
  return catalog?.servers.find((s) =>
    specifier
      ? s.registrySpecifier === specifier
      : s.remotes?.some(
          (remote) =>
            normalizeRemoteUrl(remote.url) === normalizeRemoteUrl(url ?? ""),
        ),
  );
}
