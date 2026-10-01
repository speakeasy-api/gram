import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { filterToHttpRemotes } from "@/pages/catalog/remotes";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import { ExternalMCPRemoteTransportType } from "@gram/client/models/components/externalmcpremote.js";
import type { OktaServerSuggestion } from "@gram/client/models/components/oktaserversuggestion.js";

const TRANSPORT_TYPES: ReadonlySet<string> = new Set(
  Object.values(ExternalMCPRemoteTransportType),
);

function transportType(type: string): ExternalMCPRemoteTransportType | null {
  return TRANSPORT_TYPES.has(type)
    ? (type as ExternalMCPRemoteTransportType)
    : null;
}

/**
 * Shapes a suggestion as the catalog server the install dialog takes. No
 * registry id, so the dialog installs from these remotes without a lookup.
 * Remotes with an unknown transport are dropped, and the result keeps only
 * what the install flow creates: streamable HTTP over HTTPS.
 */
export function suggestionToCatalogServer(
  suggestion: OktaServerSuggestion,
): PulseMCPServer {
  const remotes: ExternalMCPRemote[] = [];
  for (const remote of suggestion.remotes) {
    const type = transportType(remote.type);
    if (type === null) continue;
    remotes.push({
      url: remote.url,
      transportType: type,
      headers: remote.headers.map((header) => ({
        name: header.name,
        description: header.description,
        isRequired: header.isRequired,
        isSecret: header.isSecret,
      })),
    });
  }
  return filterToHttpRemotes({
    registrySpecifier: suggestion.serverName,
    title: suggestion.title ?? suggestion.serverName,
    description: suggestion.description,
    iconUrl: suggestion.iconUrl,
    remotes,
    isReadOnly: false,
    supportsDcr: false,
    toolCount: 0,
    version: "",
    meta: {},
  });
}

/** Whether the install dialog can create at least one of the suggestion's remotes. */
export function isSuggestionInstallable(
  suggestion: OktaServerSuggestion,
): boolean {
  return (suggestionToCatalogServer(suggestion).remotes?.length ?? 0) > 0;
}
