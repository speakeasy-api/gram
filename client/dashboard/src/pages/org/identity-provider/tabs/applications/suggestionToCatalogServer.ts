import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import { ExternalMCPRemoteTransportType } from "@gram/client/models/components/externalmcpremote.js";
import type { OktaServerSuggestion } from "@gram/client/models/components/oktaserversuggestion.js";

function transportType(type: string): ExternalMCPRemoteTransportType {
  return type === ExternalMCPRemoteTransportType.Sse
    ? ExternalMCPRemoteTransportType.Sse
    : ExternalMCPRemoteTransportType.StreamableHttp;
}

/**
 * Shapes a suggestion as the catalog server the install dialog takes. No
 * registry id, so the dialog installs from these remotes without a lookup.
 */
export function suggestionToCatalogServer(
  suggestion: OktaServerSuggestion,
): PulseMCPServer {
  const remotes: ExternalMCPRemote[] = suggestion.remotes.map((remote) => ({
    url: remote.url,
    transportType: transportType(remote.type),
    headers: remote.headers.map((header) => ({
      name: header.name,
      description: header.description,
      isRequired: header.isRequired,
      isSecret: header.isSecret,
    })),
  }));
  return {
    registrySpecifier: suggestion.serverName,
    title: suggestion.title ?? suggestion.serverName,
    description: suggestion.description,
    remotes,
    isReadOnly: false,
    supportsDcr: false,
    toolCount: 0,
    version: "",
    meta: {},
  };
}
