import type { ServerIdentityImpactServer } from "@gram/client/models/components/serveridentityimpactserver.js";
import { describe, expect, it } from "vitest";

import {
  groupByImpact,
  needsSharedIssuerConfirm,
  sharedIssuerChangeBlocked,
} from "./upstreamRepointing";

function server(
  id: string,
  impact: ServerIdentityImpactServer["impact"],
): ServerIdentityImpactServer {
  return {
    id,
    kind: impact === "client_removed" ? "gateway" : "mcp_server",
    impact,
    projectId: "project-1",
    projectName: "Project",
  };
}

const quiet = { servers: [], hiddenCount: 0, pending: false, failed: false };

describe("needsSharedIssuerConfirm", () => {
  it("confirms when servers are named, hidden, refused, or the check failed", () => {
    expect(needsSharedIssuerConfirm(quiet)).toBe(false);
    expect(
      needsSharedIssuerConfirm({ ...quiet, servers: [server("a", "clear")] }),
    ).toBe(true);
    expect(needsSharedIssuerConfirm({ ...quiet, hiddenCount: 1 })).toBe(true);
    expect(needsSharedIssuerConfirm({ ...quiet, failed: true })).toBe(true);
    expect(needsSharedIssuerConfirm({ ...quiet, refusal: "no" })).toBe(true);
  });

  it("does not confirm on its own while the check is pending", () => {
    expect(needsSharedIssuerConfirm({ ...quiet, pending: true })).toBe(false);
  });
});

describe("sharedIssuerChangeBlocked", () => {
  it("blocks on a refusal or on servers the user cannot see", () => {
    expect(sharedIssuerChangeBlocked(quiet)).toBe(false);
    expect(
      sharedIssuerChangeBlocked({ ...quiet, servers: [server("a", "clear")] }),
    ).toBe(false);
    expect(sharedIssuerChangeBlocked({ ...quiet, failed: true })).toBe(false);
    expect(sharedIssuerChangeBlocked({ ...quiet, hiddenCount: 1 })).toBe(true);
    expect(sharedIssuerChangeBlocked({ ...quiet, refusal: "no" })).toBe(true);
  });
});

describe("groupByImpact", () => {
  it("groups servers by impact in a stable order and drops empty groups", () => {
    const groups = groupByImpact([
      server("g", "client_removed"),
      server("a", "resignin"),
      server("b", "repoint"),
      server("c", "resignin"),
    ]);

    expect(
      groups.map(([impact, servers]) => [
        impact,
        servers.map((entry) => entry.id),
      ]),
    ).toEqual([
      ["repoint", ["b"]],
      ["resignin", ["a", "c"]],
      ["client_removed", ["g"]],
    ]);
  });
});
