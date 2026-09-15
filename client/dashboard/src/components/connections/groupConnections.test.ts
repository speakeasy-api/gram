import { describe, expect, it } from "vitest";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionUpstream } from "@gram/client/models/components/usersessionupstream.js";

import { groupConnections, splitByActivity } from "./groupConnections";

const NOW = new Date("2026-08-17T12:00:00Z").getTime();
const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;

function at(offsetMs: number): Date {
  return new Date(NOW + offsetMs);
}

function upstream(overrides: Partial<UserSessionUpstream> = {}) {
  return {
    remoteSessionId: "upstream-1",
    remoteSessionClientId: "rclient-1",
    remoteSessionIssuerId: "rissuer-1",
    issuerSlug: "api.stripe.com",
    hasRefreshToken: true,
    autoRefresh: false,
    scopes: [],
    refreshExpiresAt: at(30 * DAY),
    ...overrides,
  } as UserSessionUpstream;
}

function session(overrides: Partial<UserSession> = {}) {
  return {
    id: "session-1",
    userSessionIssuerId: "issuer-1",
    subjectUrn: "user:someone",
    subjectType: "user",
    subjectDisplayName: "Ada Lovelace",
    clientName: "Claude Code",
    jti: "jti-1",
    issuerSlug: "the-server",
    refreshExpiresAt: at(30 * DAY),
    expiresAt: at(HOUR),
    createdAt: at(-30 * DAY),
    updatedAt: at(-HOUR),
    lastUsedAt: at(-2 * HOUR),
    ...overrides,
  } as UserSession;
}

describe("groupConnections", () => {
  // The credential kind reaches an agent row from whichever source the caller
  // has: the MCP server tab hands over registration records, while the
  // organization and employee pages pass sessions alone.
  it("reads the credential kind off sessions when no registrations are passed", () => {
    const groups = groupConnections(
      [
        session({
          userSessionClientId: "client-1",
          clientCredentialKind: "key",
          clientTokenEndpointAuthMethod: "private_key_jwt",
        }),
      ],
      "client",
      { now: NOW },
    );

    expect(groups).toHaveLength(1);
    expect(groups[0]!.credentialKind).toBe("key");
    expect(groups[0]!.clientId).toBe("client-1");
  });

  it("reads the credential kind off a registration that holds no sessions", () => {
    const groups = groupConnections([], "client", {
      now: NOW,
      clients: [
        {
          id: "client-9",
          clientName: "Dormant Agent",
          credentialKind: "misconfigured",
        } as never,
      ],
    });

    expect(groups).toHaveLength(1);
    expect(groups[0]!.credentialKind).toBe("misconfigured");
    expect(groups[0]!.clientId).toBe("client-9");
  });

  // A session with no bound client is grouped under its client name, and there
  // is no registration behind it to describe or open.
  it("leaves the credential fields unset for a session with no client id", () => {
    const groups = groupConnections(
      [session({ userSessionClientId: undefined })],
      "client",
      { now: NOW },
    );

    expect(groups[0]!.clientId).toBeUndefined();
    expect(groups[0]!.credentialKind).toBeUndefined();
  });

  // Only agent rows name a registration. A person or provider row must not
  // inherit one from whichever session happened to create the group.
  it("leaves the credential fields unset under person and provider grouping", () => {
    const withClient = session({
      userSessionClientId: "client-1",
      clientCredentialKind: "key",
      upstreams: [upstream()],
    });

    for (const grouping of ["subject", "provider"] as const) {
      const groups = groupConnections([withClient], grouping, { now: NOW });
      expect(groups[0]!.clientId).toBeUndefined();
      expect(groups[0]!.credentialKind).toBeUndefined();
    }
  });

  it("files a session once per provider even with two upstreams at one issuer", () => {
    // An issuer can have several remote_session_clients attached, so a subject
    // can hold two upstreams that resolve to the same provider. Keyed on the
    // upstream rather than the issuer, that filed the session twice under one
    // heading and doubled its connection count.
    const twoClientsOneIssuer = session({
      upstreams: [
        upstream({ remoteSessionId: "u1", remoteSessionClientId: "rc1" }),
        upstream({ remoteSessionId: "u2", remoteSessionClientId: "rc2" }),
      ],
    });

    const groups = groupConnections([twoClientsOneIssuer], "provider", {
      now: NOW,
    });

    expect(groups).toHaveLength(1);
    expect(groups[0]!.label).toBe("Stripe");
    expect(groups[0]!.sessions).toHaveLength(1);
  });

  it("orders groups by their liveliest connection, unused last", () => {
    const groups = groupConnections(
      [
        session({ id: "s1", subjectUrn: "user:a", lastUsedAt: at(-3 * DAY) }),
        session({ id: "s2", subjectUrn: "user:b", lastUsedAt: at(-1 * HOUR) }),
        session({ id: "s3", subjectUrn: "user:c", lastUsedAt: undefined }),
      ],
      "subject",
      { now: NOW },
    );

    expect(groups.map((group) => group.key)).toEqual([
      "user:b",
      "user:a",
      "user:c",
    ]);
  });
});

describe("splitByActivity", () => {
  it("separates the dormant and the unusable from connections still in use", () => {
    const groups = groupConnections(
      [
        session({ id: "s1", subjectUrn: "user:live", lastUsedAt: at(-HOUR) }),
        // Dormant: healthy credentials, untouched for over a week.
        session({
          id: "s2",
          subjectUrn: "user:dormant",
          lastUsedAt: at(-10 * DAY),
        }),
        // Unusable: used minutes ago, but its own refresh window has passed.
        session({
          id: "s3",
          subjectUrn: "user:expired",
          lastUsedAt: at(-5 * 60 * 1000),
          refreshExpiresAt: at(-DAY),
        }),
      ],
      "subject",
      { now: NOW },
    );

    const { active, inactive } = splitByActivity(groups);

    expect(active.map((group) => group.key)).toEqual(["user:live"]);
    expect(inactive.map((group) => group.key).sort()).toEqual([
      "user:dormant",
      "user:expired",
    ]);
  });
});
