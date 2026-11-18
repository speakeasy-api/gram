// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  blockCredentialRow,
  type CredentialRef,
  credentialRowState,
  type CredentialRule,
  credentialRuleChanges,
  credentialRuleKey,
  credentialUsable,
  otherCredentialRules,
  rebaseCredentialRules,
  REPRESENTED_USER,
  revokeCredentialRow,
  splitCredentialRow,
} from "./credentialAccessModel";

const client = (
  id: string,
  clientType: string,
  servers: string[] = ["srv-1"],
): CredentialRef => ({
  id,
  name: id,
  clientType,
  enabled: true,
  remoteSessionClientId:
    clientType === "remote_session" ? `up-${id}` : undefined,
  servers: servers.map((serverId) => ({ id: serverId, projectId: "p1" })),
});

const clients = [
  client("oauth-a", "remote_session"),
  client("sa-a", "service_account"),
  client("sa-b", "service_account", ["srv-2"]),
];

const allowAll: CredentialRule = {
  scope: "mcp:auth",
  selector: { resourceKind: "mcp_auth_client", resourceId: "*" },
};

const keys = (rules: CredentialRule[]) => rules.map(credentialRuleKey).sort();

describe("credential rows", () => {
  it("reads a blanket allow as on for every credential, inherited below", () => {
    expect(credentialRowState([allowAll], { kind: "all" }).state).toBe("on");
    expect(
      credentialRowState([allowAll], {
        kind: "type",
        clientType: "service_account",
      }).state,
    ).toBe("inherited");
    expect(
      credentialRowState([allowAll], {
        kind: "client",
        clientId: "sa-a",
        clientType: "service_account",
      }).state,
    ).toBe("inherited");
  });

  it("lets a block on a kind win over an allow on every credential", () => {
    const rules = blockCredentialRow([allowAll], {
      kind: "type",
      clientType: "service_account",
    });
    expect(
      credentialRowState(rules, {
        kind: "client",
        clientId: "sa-a",
        clientType: "service_account",
      }).state,
    ).toBe("inherited-blocked");
  });

  it("splits every credential into one allow per kind", () => {
    const rules = splitCredentialRow([allowAll], { kind: "all" }, clients);
    expect(credentialRowState(rules, { kind: "all" }).state).toBe("off");
    expect(
      credentialRowState(rules, { kind: "type", clientType: "ema" }).state,
    ).toBe("on");
    // The personal kind is written for the person's own connection.
    expect(rules).toContainEqual({
      scope: "mcp:auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "remote_session",
        connectionOwner: REPRESENTED_USER,
      },
    });
  });

  it("takes one credential away from a blanket allow and keeps the rest", () => {
    const sa = {
      kind: "client",
      clientId: "sa-a",
      clientType: "service_account",
    } as const;
    const rules = revokeCredentialRow([allowAll], sa, clients);

    expect(credentialRowState(rules, sa).state).toBe("off");
    for (const other of clients.filter((c) => c.id !== "sa-a")) {
      expect(credentialUsable(rules, other, "p1")).toBe("allowed");
    }
    expect(credentialUsable(rules, clients[1]!, "p1")).toBe("none");
    // Every other kind is still covered as a kind, so later ones stay usable.
    expect(
      credentialRowState(rules, { kind: "type", clientType: "pass_through" })
        .state,
    ).toBe("on");
  });
});

describe("credentialUsable", () => {
  it("never reads a rule naming another person's connection as usable", () => {
    const rules: CredentialRule[] = [
      {
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          clientType: "remote_session",
          connectionOwner: "user-2",
        },
      },
    ];
    expect(credentialUsable(rules, clients[0]!, "p1")).toBe("none");
  });

  it("applies a project-bound rule only in its project", () => {
    const rules: CredentialRule[] = [
      {
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "sa-a",
          projectId: "p1",
        },
      },
    ];
    expect(credentialUsable(rules, clients[1]!, "p1")).toBe("allowed");
    expect(credentialUsable(rules, clients[1]!, "p2")).toBe("none");
  });

  it("does not match a shared credential with a personal-only key", () => {
    const rules: CredentialRule[] = [
      {
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "*",
          connectionOwner: REPRESENTED_USER,
        },
      },
    ];
    expect(credentialUsable(rules, clients[0]!, "p1")).toBe("allowed");
    expect(credentialUsable(rules, clients[1]!, "p1")).toBe("none");
  });
});

describe("other rules", () => {
  it("keeps project-bound rules and rules for removed credentials apart", () => {
    const projectBound: CredentialRule = {
      scope: "mcp:auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "sa-a",
        projectId: "p1",
      },
    };
    const removed: CredentialRule = {
      scope: "mcp:auth",
      selector: { resourceKind: "mcp_auth_client", resourceId: "gone" },
    };
    expect(
      otherCredentialRules([allowAll, projectBound, removed], clients),
    ).toEqual([projectBound, removed]);
    // Until the credentials load, a named one cannot be called removed.
    expect(otherCredentialRules([removed], null)).toEqual([]);
  });
});

describe("saving", () => {
  it("adds and removes only what changed", () => {
    const next = splitCredentialRow([allowAll], { kind: "all" }, clients);
    const { add, remove } = credentialRuleChanges([allowAll], next);
    expect(remove).toEqual([allowAll]);
    expect(add).toHaveLength(5);
  });

  it("keeps the person's edits and follows the store for everything else", () => {
    const gained: CredentialRule = {
      scope: "mcp:blocked_auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "ema",
      },
    };
    const current = splitCredentialRow([allowAll], { kind: "all" }, clients);
    const rebased = rebaseCredentialRules({
      current,
      baseline: [allowAll],
      stored: [allowAll, gained],
    });
    expect(keys(rebased)).toEqual(keys([...current, gained]));
  });
});

describe("rules the tree cannot show", () => {
  it("lists an owner-constrained rule on every credential as other", () => {
    const ownOnly: CredentialRule = {
      scope: "mcp:auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        connectionOwner: REPRESENTED_USER,
      },
    };
    expect(credentialRowState([ownOnly], { kind: "all" }).state).toBe("off");
    expect(otherCredentialRules([ownOnly], clients)).toEqual([ownOnly]);
  });

  it("keeps a personal credential's own tick on its row", () => {
    const row = {
      kind: "client",
      clientId: "oauth-a",
      clientType: "remote_session",
    } as const;
    const rules = splitCredentialRow(
      splitCredentialRow([allowAll], { kind: "all" }, clients),
      { kind: "type", clientType: "remote_session" },
      clients,
    );
    expect(credentialRowState(rules, row).state).toBe("on");
    expect(otherCredentialRules(rules, clients)).toEqual([]);
  });

  it("lists rules for a kind this dashboard does not know as other", () => {
    const future: CredentialRule = {
      scope: "mcp:auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "*",
        clientType: "future_kind",
      },
    };
    const futureClient = client("future-a", "future_kind");
    const named: CredentialRule = {
      scope: "mcp:auth",
      selector: { resourceKind: "mcp_auth_client", resourceId: "future-a" },
    };
    expect(
      otherCredentialRules([future, named], [...clients, futureClient]),
    ).toEqual([future, named]);
  });
});

describe("forward compatibility", () => {
  it("reads a personal tick on one credential without a kind as that row", () => {
    const rule: CredentialRule = {
      scope: "mcp:auth",
      selector: {
        resourceKind: "mcp_auth_client",
        resourceId: "oauth-a",
        connectionOwner: REPRESENTED_USER,
      },
    };
    const row = {
      kind: "client",
      clientId: "oauth-a",
      clientType: "remote_session",
    } as const;
    expect(credentialRowState([rule], row).state).toBe("on");
    expect(revokeCredentialRow([rule], row, clients)).toEqual([]);
  });

  it("keeps a credential of an unknown kind when splitting every credential", () => {
    const future = client("future-a", "future_kind");
    const rules = splitCredentialRow([allowAll], { kind: "all" }, [
      ...clients,
      future,
    ]);
    expect(credentialUsable(rules, future, "p1")).toBe("allowed");
  });
});
