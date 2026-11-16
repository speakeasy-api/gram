// @vitest-environment node
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import type { TemporaryGrant } from "@gram/client/models/components/temporarygrant.js";
import { describe, expect, it } from "vitest";
import {
  activeTemporaryGrants,
  challengesAnsweredBy,
  formatTimeLeft,
  temporaryAccessDeadline,
  temporaryGrantCovers,
  temporaryGrantTarget,
} from "./temporary-access-helpers";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

function challenge(overrides: Partial<AuthzChallenge>): AuthzChallenge {
  return {
    id: "c1",
    timestamp: new Date(),
    organizationId: "org",
    principalUrn: "user:me",
    principalType: "user",
    operation: "require",
    outcome: "deny",
    reason: "no_grants",
    scope: "chat:read",
    resourceKind: "chat",
    resourceId: "chat-1",
    selector: { resource_kind: "chat", resource_id: "chat-1" },
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    ...overrides,
  } as AuthzChallenge;
}

const grantFor = {
  principalUrn: "user:me",
  scope: "chat:read",
  resourceKind: "chat",
  resourceId: "chat-1",
};

describe("temporaryAccessDeadline", () => {
  const morning = new Date(2026, 10, 12, 9, 0, 0);

  it("adds fixed durations to now", () => {
    expect(temporaryAccessDeadline("30m", morning)?.getTime()).toBe(
      morning.getTime() + 30 * MINUTE,
    );
    expect(temporaryAccessDeadline("2h", morning)?.getTime()).toBe(
      morning.getTime() + 2 * HOUR,
    );
  });

  it("keeps 24 hours inside the server's maximum", () => {
    const deadline = temporaryAccessDeadline("24h", morning)!;
    expect(deadline.getTime() - morning.getTime()).toBeLessThan(24 * HOUR);
  });

  it("ends the day at 23:59 local time", () => {
    const deadline = temporaryAccessDeadline("eod", morning)!;
    expect(deadline.getHours()).toBe(23);
    expect(deadline.getMinutes()).toBe(59);
    expect(deadline.getDate()).toBe(morning.getDate());
  });

  it("offers no end of day when the day is almost over", () => {
    const lateNight = new Date(2026, 10, 12, 23, 55, 0);
    expect(temporaryAccessDeadline("eod", lateNight)).toBeNull();
  });
});

describe("challengesAnsweredBy", () => {
  it("picks the viewer's unresolved denials on the granted resource", () => {
    expect(
      challengesAnsweredBy(
        [
          challenge({ id: "match" }),
          challenge({ id: "other-chat", resourceId: "chat-2" }),
          challenge({ id: "someone-else", principalUrn: "user:other" }),
          challenge({ id: "other-scope", scope: "log:read" }),
          challenge({ id: "allowed", outcome: "allow" }),
          challenge({ id: "resolved", resolutionType: "dismissed" }),
          challenge({ id: "legacy", selector: undefined }),
        ],
        grantFor,
      ),
    ).toEqual(["match"]);
  });

  it("answers denials on any chat when the grant covers every chat", () => {
    expect(
      challengesAnsweredBy(
        [challenge({ id: "a" }), challenge({ id: "b", resourceId: "chat-2" })],
        { ...grantFor, resourceId: "*" },
      ),
    ).toEqual(["a", "b"]);
  });
});

describe("temporaryGrantCovers", () => {
  const grant = (resourceId: string, status = "active") =>
    ({
      id: "g",
      scope: "chat:read",
      status,
      selector: { resourceKind: "chat", resourceId },
    }) as TemporaryGrant;

  it("matches the granted chat or a grant on every chat", () => {
    expect(temporaryGrantCovers(grant("chat-1"), "chat:read", "chat-1")).toBe(
      true,
    );
    expect(temporaryGrantCovers(grant("*"), "chat:read", "chat-9")).toBe(true);
    expect(temporaryGrantCovers(grant("chat-1"), "chat:read", "chat-2")).toBe(
      false,
    );
  });

  it("ignores ended grants and other scopes", () => {
    expect(
      temporaryGrantCovers(grant("*", "expired"), "chat:read", "chat-1"),
    ).toBe(false);
    expect(temporaryGrantCovers(grant("*"), "log:read", "chat-1")).toBe(false);
  });
});

describe("formatTimeLeft", () => {
  const now = new Date(2026, 10, 12, 9, 0, 0);
  const at = (ms: number) => new Date(now.getTime() + ms);

  it("reads hours and minutes", () => {
    expect(formatTimeLeft(at(72 * MINUTE), now)).toBe("1 h 12 min");
    expect(formatTimeLeft(at(2 * HOUR), now)).toBe("2 h");
    expect(formatTimeLeft(at(45 * MINUTE + 30_000), now)).toBe("45 min");
    expect(formatTimeLeft(at(20_000), now)).toBe("under a minute");
  });
});

describe("temporaryGrantTarget", () => {
  const grant = (resourceKind: string, resourceId: string) =>
    ({ selector: { resourceKind, resourceId } }) as TemporaryGrant;

  it("names every resource or one by short id", () => {
    expect(temporaryGrantTarget(grant("chat", "*"))).toBe("All sessions");
    expect(
      temporaryGrantTarget(
        grant("chat", "0f1e2d3c-0000-4000-8000-000000000001"),
      ),
    ).toBe("Session 0f1e2d3c");
    expect(temporaryGrantTarget(grant("project", "*"))).toBe("All projects");
  });
});

describe("activeTemporaryGrants", () => {
  const now = new Date(2026, 10, 12, 9, 0, 0);
  const grant = (id: string, status: string, minutes: number) =>
    ({
      id,
      status,
      expiresAt: new Date(now.getTime() + minutes * MINUTE),
    }) as TemporaryGrant;

  it("keeps grants still running, soonest first", () => {
    expect(
      activeTemporaryGrants(
        [
          grant("later", "active", 90),
          grant("ended", "expired", -5),
          grant("lapsed", "active", -1),
          grant("soon", "active", 10),
        ],
        now,
      ).map((g) => g.id),
    ).toEqual(["soon", "later"]);
  });
});
