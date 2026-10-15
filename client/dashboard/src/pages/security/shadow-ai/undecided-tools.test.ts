import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import {
  countUndecidedAITools,
  countUndecidedMCPServers,
} from "./undecided-tools";

function detection(
  access: Partial<NonNullable<AIDetection["access"]>> | undefined,
): AIDetection {
  return {
    targetId: "t",
    displayName: "T",
    category: "harness",
    userCount: 1,
    deviceCount: 1,
    signals: ["installed"],
    versions: [],
    firstSeen: new Date(0),
    lastSeen: new Date(0),
    access: access
      ? {
          state: stateOf(access.decision),
          decision: "unreviewed",
          enforceable: true,
          ...access,
        }
      : undefined,
  };
}

/** The state the API reports for a decision, so fixtures stay within the
 *  contract: approved reads allowed, blocked reads blocked. */
function stateOf(
  decision: NonNullable<AIDetection["access"]>["decision"] | undefined,
): NonNullable<AIDetection["access"]>["state"] {
  if (decision === "approved") return "allowed";
  if (decision === "blocked") return "blocked";
  return "unreviewed";
}

function server(
  status:
    | NonNullable<ShadowMCPInventoryServer["approvalRequest"]>["status"]
    | undefined,
  standingDecision?: "approved" | "denied",
): ShadowMCPInventoryServer {
  return {
    approvalRequest: status
      ? ({
          status,
          standingDecision,
        } as ShadowMCPInventoryServer["approvalRequest"])
      : undefined,
  } as ShadowMCPInventoryServer;
}

describe("countUndecidedAITools", () => {
  it("counts enforceable tools with no decision only", () => {
    expect(
      countUndecidedAITools([
        detection({ decision: "unreviewed" }),
        detection({ decision: "approved" }),
        detection({ decision: "blocked" }),
        detection({ decision: "unreviewed", enforceable: false }),
        detection(undefined),
      ]),
    ).toBe(1);
  });
});

describe("countUndecidedMCPServers", () => {
  it("counts servers with no review or a review still waiting", () => {
    expect(
      countUndecidedMCPServers([
        server(undefined),
        server("unreviewed"),
        server("requested"),
        // Reopened with a decision still standing: enforced, not a backlog.
        server("requested", "denied"),
        server("approved"),
        server("denied"),
        server("superseded"),
      ]),
    ).toBe(3);
  });
});
