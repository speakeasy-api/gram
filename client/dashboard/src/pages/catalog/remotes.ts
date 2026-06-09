import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ExternalMCPRemote } from "@gram/client/models/components";

export function filterToHttpRemotes(server: PulseMCPServer): PulseMCPServer {
  const httpRemotes = server.remotes?.filter(
    (r) => r.transportType === "streamable-http",
  );
  return {
    ...server,
    remotes: httpRemotes ? dedupeRemotesByUrl(httpRemotes) : httpRemotes,
  };
}

// Some registry entries publish multiple remotes with the same URL that differ
// only by their `headers` array (e.g. one variant for OAuth, another for
// static API-key auth). At deploy time the backend re-fetches from the
// registry and picks the first matching URL, so the second variant is
// unreachable today — collapse the duplicate so users do not see two
// identical-looking checkboxes.
export function dedupeRemotesByUrl(
  remotes: ExternalMCPRemote[],
): ExternalMCPRemote[] {
  const byUrl = new Map<string, ExternalMCPRemote>();
  for (const r of remotes) {
    if (!byUrl.has(r.url)) byUrl.set(r.url, r);
  }
  return Array.from(byUrl.values());
}
