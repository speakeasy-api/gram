import { describe, expect, it } from "vitest";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionUpstream } from "@gram/client/models/components/usersessionupstream.js";

import {
  connectionActivityLabel,
  connectionState,
} from "@/lib/connection-state";

const NOW = new Date("2026-08-17T12:00:00Z").getTime();

// The generated SDK deserializes date-time fields to Date, and models an
// absent one as undefined rather than null.
function at(offsetMs: number): Date {
  return new Date(NOW + offsetMs);
}

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;

function upstream(overrides: Partial<UserSessionUpstream> = {}) {
  return {
    remoteSessionId: "upstream-1",
    remoteSessionClientId: "client-1",
    remoteSessionIssuerId: "issuer-1",
    issuerSlug: "mcp.linear.app",
    hasRefreshToken: true,
    autoRefresh: false,
    scopes: [],
    accessExpiresAt: at(HOUR),
    refreshExpiresAt: at(30 * DAY),
    authorizationExpiresAt: undefined,
    lastUsedAt: at(-60 * 1000),
    ...overrides,
  } as UserSessionUpstream;
}

function session(overrides: Partial<UserSession> = {}) {
  return {
    id: "session-1",
    userSessionIssuerId: "issuer-1",
    subjectUrn: "user:someone",
    jti: "jti-1",
    refreshExpiresAt: at(30 * DAY),
    expiresAt: at(HOUR),
    createdAt: at(-30 * DAY),
    updatedAt: at(-HOUR),
    issuerSlug: "linear-mcp",
    subjectType: "user",
    upstreams: [],
    lastUsedAt: at(-60 * 1000),
    ...overrides,
  } as UserSession;
}

describe("connectionState", () => {
  it("reports a recently used, healthy connection as live", () => {
    expect(connectionState(session({ upstreams: [upstream()] }), NOW)).toBe(
      "live",
    );
  });

  it("reports a healthy but dormant connection as idle", () => {
    expect(connectionState(session({ lastUsedAt: at(-2 * DAY) }), NOW)).toBe(
      "idle",
    );
  });

  it("treats an unrecorded last use as idle rather than live", () => {
    // Sessions predating the column report null. Unknown must not be optimistic.
    expect(connectionState(session({ lastUsedAt: undefined }), NOW)).toBe(
      "idle",
    );
  });

  it("reports revoked ahead of every other signal", () => {
    const revoked = session({
      revokedAt: at(-HOUR),
      refreshExpiresAt: at(-DAY),
      lastUsedAt: at(-60 * 1000),
    });
    expect(connectionState(revoked, NOW)).toBe("revoked");
  });

  it("reports an expired session as needing re-auth even if recently used", () => {
    const expired = session({
      refreshExpiresAt: at(-HOUR),
      lastUsedAt: at(-60 * 1000),
    });
    expect(connectionState(expired, NOW)).toBe("needs_reauth");
  });

  it("warns when the session's own refresh deadline is close", () => {
    expect(
      connectionState(session({ refreshExpiresAt: at(6 * HOUR) }), NOW),
    ).toBe("expiring");
  });

  // The inbound leg being healthy says nothing about whether Gram can still
  // reach the upstream — this is the case a session-only view cannot express.
  it("reports needs_reauth when an upstream has lost its grant", () => {
    const broken = session({
      upstreams: [
        upstream({ hasRefreshToken: false, accessExpiresAt: at(-HOUR) }),
      ],
    });
    expect(connectionState(broken, NOW)).toBe("needs_reauth");
  });

  it("warns on an upstream authorization deadline even while refreshing happily", () => {
    // authorization_expires_at does not slide on refresh, so a connection that
    // looks perfectly healthy can still be hours from forcing re-consent.
    const nearingDeadline = session({
      upstreams: [
        upstream({
          hasRefreshToken: true,
          refreshExpiresAt: at(30 * DAY),
          authorizationExpiresAt: at(3 * HOUR),
        }),
      ],
    });
    expect(connectionState(nearingDeadline, NOW)).toBe("expiring");
  });

  it("needs re-auth once the authorization deadline passes without a refresh grant", () => {
    // The absolute authorization deadline is the point the user must consent
    // again, and no token exchange moves it. Checked only inside the
    // has-refresh-token branch, an upstream with a live access token and a
    // lapsed authorization read as merely expiring — the one state that says
    // "still works", for a connection that does not.
    const lapsedAuthorization = session({
      upstreams: [
        upstream({
          hasRefreshToken: false,
          accessExpiresAt: at(HOUR),
          refreshExpiresAt: undefined,
          authorizationExpiresAt: at(-DAY),
        }),
      ],
    });
    expect(connectionState(lapsedAuthorization, NOW)).toBe("needs_reauth");
  });

  it("does not warn on a non-expiring upstream", () => {
    // Upstreams like Slack issue tokens with no expiry; null must read as
    // "never expires", not as a deadline at the epoch.
    const nonExpiring = session({
      upstreams: [
        upstream({
          accessExpiresAt: undefined,
          refreshExpiresAt: undefined,
          authorizationExpiresAt: undefined,
        }),
      ],
    });
    expect(connectionState(nonExpiring, NOW)).toBe("live");
  });
});

describe("connectionActivityLabel", () => {
  it("distinguishes no recorded use from a known idle period", () => {
    expect(connectionActivityLabel(undefined)).toBe("no recorded use");
    expect(connectionActivityLabel(at(-2 * HOUR))).toContain("used");
  });
});
