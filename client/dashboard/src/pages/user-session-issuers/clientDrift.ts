import type { UserSessionIssuerRemoteSessionClient } from "@gram/client/models/components/usersessionissuerremotesessionclient.js";
import type { UserSessionIssuerRemoteSessionClientBindings } from "@gram/client/models/components/usersessionissuerremotesessionclientbindings.js";

/** An MCP server that uses a client, named for display. */
type DriftServer = { id: string; name: string; projectName: string };

/** One client, and where it appears: bound to the provider, used by servers, or both. */
export type ClientUse = {
  client: UserSessionIssuerRemoteSessionClient;
  bound: boolean;
  servers: DriftServer[];
};

export type ClientDrift = {
  /** Every client either side names, mismatches first. */
  clients: ClientUse[];
  /** Bound to the provider, but no MCP server uses it. */
  unused: number;
  /** Used by an MCP server, but not bound to the provider. */
  unbound: number;
};

/** A client drifts when only one side names it. */
export function drifts(use: ClientUse): boolean {
  return use.bound !== use.servers.length > 0;
}

/**
 * Compares the clients a provider binds with those its MCP servers use.
 * Servers without any client are not drift: they name nothing to compare.
 */
export function compareClients(
  bindings: UserSessionIssuerRemoteSessionClientBindings,
): ClientDrift {
  const byId = new Map<string, ClientUse>();
  for (const client of bindings.boundClients) {
    byId.set(client.id, { client, bound: true, servers: [] });
  }
  for (const entry of bindings.serverClients) {
    const client = entry.remoteSessionClient;
    let use = byId.get(client.id);
    if (!use) {
      use = { client, bound: false, servers: [] };
      byId.set(client.id, use);
    }
    if (!use.servers.some((server) => server.id === entry.mcpServerId)) {
      use.servers.push({
        id: entry.mcpServerId,
        name: entry.mcpServerName,
        projectName: entry.projectName,
      });
    }
  }

  const clients = [...byId.values()].sort(
    (a, b) =>
      Number(drifts(b)) - Number(drifts(a)) ||
      clientLabel(a.client).localeCompare(clientLabel(b.client)),
  );
  return {
    clients,
    unused: clients.filter((use) => use.bound && use.servers.length === 0)
      .length,
    unbound: clients.filter((use) => !use.bound && use.servers.length > 0)
      .length,
  };
}

/** How a client reads in the UI: its authorization server, then its id. */
function clientLabel(client: UserSessionIssuerRemoteSessionClient): string {
  return `${client.remoteSessionIssuerName} · ${client.clientId}`;
}
