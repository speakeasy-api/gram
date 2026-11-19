// @vitest-environment node
import type { UserSessionIssuerRemoteSessionClient } from "@gram/client/models/components/usersessionissuerremotesessionclient.js";
import { describe, expect, it } from "vitest";
import { compareClients, drifts } from "./clientDrift";

function client(id: string): UserSessionIssuerRemoteSessionClient {
  return {
    id,
    clientId: `${id}-cid`,
    remoteSessionIssuerId: `${id}-as`,
    remoteSessionIssuerName: id,
    remoteSessionIssuerUrl: `https://${id}.example.com`,
  };
}

function uses(serverId: string, id: string) {
  return {
    mcpServerId: serverId,
    mcpServerName: serverId,
    projectId: "project-1",
    projectName: "Default",
    remoteSessionClient: client(id),
  };
}

describe("compareClients", () => {
  it("finds no drift when both sides name the same clients", () => {
    const result = compareClients({
      boundClients: [client("github")],
      serverClients: [uses("server-a", "github"), uses("server-b", "github")],
    });
    expect(result.unused).toBe(0);
    expect(result.unbound).toBe(0);
    expect(result.clients).toHaveLength(1);
    expect(result.clients[0]?.servers.map((s) => s.id)).toEqual([
      "server-a",
      "server-b",
    ]);
  });

  it("counts bound clients no server uses, and server clients not bound", () => {
    const result = compareClients({
      boundClients: [client("github"), client("unused")],
      serverClients: [uses("server-a", "github"), uses("server-a", "stray")],
    });
    expect(result.unused).toBe(1);
    expect(result.unbound).toBe(1);
    // Mismatches sort first.
    expect(result.clients.map((use) => use.client.id)).toEqual([
      "stray",
      "unused",
      "github",
    ]);
    expect(result.clients.filter(drifts).map((use) => use.client.id)).toEqual([
      "stray",
      "unused",
    ]);
  });

  it("lists a server once per client even if named twice", () => {
    const result = compareClients({
      boundClients: [],
      serverClients: [uses("server-a", "stray"), uses("server-a", "stray")],
    });
    expect(result.clients[0]?.servers).toHaveLength(1);
  });
});
