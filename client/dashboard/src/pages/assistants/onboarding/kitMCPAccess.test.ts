import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { expect, it } from "vitest";
import { agentConnectAccess, directServerConnectGrants } from "./kitMCPAccess";

function grant(
  id: string,
  patch: Partial<AgentPolicyGrant["selector"]> = {},
  scope = "mcp:connect",
): AgentPolicyGrant {
  return {
    id,
    effect: "allow",
    scope,
    selector: {
      resourceKind: "mcp",
      resourceId: "server",
      projectId: "project",
      ...patch,
    },
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

it("selects only unconditioned Connect grants on this exact server and project", () => {
  const grants = [
    grant("direct"),
    grant("direct without project", { projectId: undefined }),
    grant("different server", { resourceId: "other" }),
    grant("different project", { projectId: "other" }),
    grant("all servers", { resourceId: "*" }),
    grant("one tool", { tool: "search" }),
    grant("disposition", { disposition: "read_only" }),
    grant("sign-in", { clientType: "remote_session" }, "mcp:auth"),
    grant("read", {}, "mcp:read"),
    grant("block", {}, "mcp:blocked_connect"),
    grant("unrelated", { resourceKind: "environment" }, "environment:read"),
  ];
  expect(
    directServerConnectGrants(grants, "project", "server").map((g) => g.id),
  ).toEqual(["direct", "direct without project"]);
});

const role: ResourceAudienceEntry = {
  principalUrn: "role:research",
  displayName: "Research",
  kind: "role",
  appliesTo: "resource",
  level: "use",
  agentIds: ["assistant-agent"],
};

const direct: ResourceAudienceEntry = {
  principalUrn: "agent:assistant-agent",
  displayName: "Assistant",
  kind: "agent",
  appliesTo: "resource",
  level: "use",
};

it("shows role access even when the agent has no direct rule", () => {
  expect(agentConnectAccess([role], "assistant-agent")).toBe(
    "All tools · via Research",
  );
  expect(agentConnectAccess([role], "another-agent")).toBe("No access");
});

it("shows a broader agent grant and inheritance alongside a direct rule", () => {
  expect(
    agentConnectAccess(
      [{ ...direct, appliesTo: "all_resources" }],
      "assistant-agent",
    ),
  ).toBe("All tools · via a broader agent grant");
  expect(agentConnectAccess([direct, role], "assistant-agent")).toBe(
    "All tools · via Research",
  );
});

it("does not let a direct agent grant override role or agent blocks", () => {
  expect(
    agentConnectAccess(
      [direct, { ...role, level: "blocked" }],
      "assistant-agent",
    ),
  ).toBe("Blocked by Research");
  expect(
    agentConnectAccess(
      [direct, { ...direct, level: "blocked" }],
      "assistant-agent",
    ),
  ).toBe("Blocked");
});
