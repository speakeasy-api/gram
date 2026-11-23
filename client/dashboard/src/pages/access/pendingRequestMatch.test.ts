import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { describe, expect, it } from "vitest";
import { pendingRequestForBucket } from "./pendingRequestMatch";

const NOW = new Date("2026-11-23T10:00:00Z");
const DAY = 24 * 60 * 60 * 1000;

function bucket(overrides: Partial<ChallengeBucket> = {}): ChallengeBucket {
  return {
    id: "b1",
    challengeCount: 1,
    challengeIds: ["c1"],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    firstSeen: NOW,
    lastSeen: NOW,
    operation: "require",
    organizationId: "org",
    outcome: "deny",
    principalType: "user",
    principalUrn: "user:ada",
    reason: "no_grants",
    scope: "log:read",
    resourceKind: "project",
    resourceId: "p1",
    roleSlugs: [],
    ...overrides,
  };
}

function request(overrides: Partial<AccessRequest> = {}): AccessRequest {
  return {
    id: "r1",
    createdAt: NOW,
    updatedAt: NOW,
    expiresAt: new Date(NOW.getTime() + DAY),
    requesterName: "Ada",
    requesterUserId: "ada",
    scope: "log:read",
    selector: { resourceKind: "project", resourceId: "p1" },
    status: "pending",
    ...overrides,
  } as AccessRequest;
}

describe("pendingRequestForBucket", () => {
  it("matches the member's pending request for the same scope and resource", () => {
    expect(pendingRequestForBucket(bucket(), [request()])?.id).toBe("r1");
  });

  it("matches a request that covers every resource", () => {
    const wide = request({
      selector: { resourceKind: "project", resourceId: "*" },
    } as Partial<AccessRequest>);
    expect(pendingRequestForBucket(bucket(), [wide])?.id).toBe("r1");
  });

  it.each([
    ["another member", { requesterUserId: "bob" }],
    ["another scope", { scope: "chat:read" }],
    ["a request no longer pending", { status: "approved" }],
    [
      "another resource",
      { selector: { resourceKind: "project", resourceId: "p2" } },
    ],
    [
      "another resource kind",
      { selector: { resourceKind: "mcp", resourceId: "p1" } },
    ],
  ])("ignores %s", (_, overrides) => {
    expect(
      pendingRequestForBucket(bucket(), [
        request(overrides as Partial<AccessRequest>),
      ]),
    ).toBeUndefined();
  });

  it("ignores rows that are not open denials of a person", () => {
    expect(
      pendingRequestForBucket(bucket({ outcome: "allow" }), [request()]),
    ).toBeUndefined();
    expect(
      pendingRequestForBucket(bucket({ resolvedAt: NOW }), [request()]),
    ).toBeUndefined();
    expect(
      pendingRequestForBucket(
        bucket({ principalType: "agent", principalUrn: "agent:ada" }),
        [request()],
      ),
    ).toBeUndefined();
  });

  it("ignores a denial from more than a week before the request", () => {
    const old = bucket({ lastSeen: new Date(NOW.getTime() - 8 * DAY) });
    expect(pendingRequestForBucket(old, [request()])).toBeUndefined();
  });

  it("picks the newest request when several match", () => {
    const older = request({
      id: "old",
      createdAt: new Date(NOW.getTime() - DAY),
    });
    const newer = request({ id: "new" });
    expect(pendingRequestForBucket(bucket(), [older, newer])?.id).toBe("new");
  });
});
