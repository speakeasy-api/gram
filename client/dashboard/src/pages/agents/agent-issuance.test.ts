import { describe, expect, it } from "vitest";

import { agentIssuanceBlocked } from "./AgentAPIKeys";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

/** Only the fields the rule reads. */
function agent(overrides: Partial<ManagedAgent> = {}): ManagedAgent {
  return {
    id: "00000000-0000-0000-0000-000000000000",
    name: "Release assistant",
    ownerUserId: "user_owner",
    lifecycle: "active",
    permissions: { read: true, write: true, authorize: true, transfer: true },
    createdAt: new Date(),
    updatedAt: new Date(),
    ...overrides,
  } as ManagedAgent;
}

describe("agentIssuanceBlocked", () => {
  it("allows issuance for an active agent the caller may authorize", () => {
    expect(agentIssuanceBlocked(agent(), true)).toBeNull();
  });

  // Each of these was a way the provisioning review's button could be offered
  // while the panel that performs the issue refused it.
  it("blocks when the credential rollout is off", () => {
    expect(agentIssuanceBlocked(agent(), false)).toMatch(/not enabled/i);
  });

  it("blocks without the authorize permission", () => {
    const denied = agent({
      permissions: {
        read: true,
        write: false,
        authorize: false,
        transfer: false,
      },
    });
    expect(agentIssuanceBlocked(denied, true)).toMatch(
      /do not have permission/i,
    );
  });

  it("names the lifecycle that blocks it", () => {
    expect(agentIssuanceBlocked(agent({ lifecycle: "suspended" }), true)).toBe(
      "This agent is suspended, so it cannot be issued new keys.",
    );
    expect(agentIssuanceBlocked(agent({ lifecycle: "revoked" }), true)).toBe(
      "This agent is revoked, so it cannot be issued new keys.",
    );
  });

  it("blocks while the agent is waiting for a new owner", () => {
    const orphaned = agent({ ownerReassignmentRequiredAt: new Date() });
    expect(agentIssuanceBlocked(orphaned, true)).toMatch(/new owner/i);
  });

  // The rollout is checked before anything else: an organization without the
  // feature should not be told it is a permission problem.
  it("reports the rollout before the permission", () => {
    const denied = agent({
      permissions: {
        read: true,
        write: false,
        authorize: false,
        transfer: false,
      },
    });
    expect(agentIssuanceBlocked(denied, false)).toMatch(/not enabled/i);
  });
});
