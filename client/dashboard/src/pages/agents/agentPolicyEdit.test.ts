import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { describe, expect, it } from "vitest";

import {
  CONNECT_SCOPE,
  grantChanges,
  grantedServers,
  hasWildcardConnectGrant,
} from "./agentPolicyEdit";

function grant(
  id: string,
  resourceId: string,
  projectId = "project-1",
): AgentPolicyGrant {
  return {
    id,
    agentId: "agent-1",
    effect: "allow",
    scope: CONNECT_SCOPE,
    selector: { resourceKind: "mcp", resourceId, projectId },
  } as unknown as AgentPolicyGrant;
}

describe("grantedServers", () => {
  it("keeps every grant that reaches the same server", () => {
    const servers = grantedServers([
      grant("g1", "server-a"),
      grant("g2", "server-a"),
    ]);
    expect(servers.get("server-a")?.grantIds).toEqual(["g1", "g2"]);
  });

  it("leaves a wildcard out of the selection", () => {
    expect([...grantedServers([grant("g1", "*")]).keys()]).toEqual([]);
  });
});

describe("hasWildcardConnectGrant", () => {
  it("reports a wildcard the selection cannot show", () => {
    expect(hasWildcardConnectGrant([grant("g1", "*")])).toBe(true);
    expect(hasWildcardConnectGrant([grant("g1", "server-a")])).toBe(false);
  });
});

describe("grantChanges", () => {
  it("removes all of a deselected server's grants", () => {
    const granted = grantedServers([
      grant("g1", "server-a"),
      grant("g2", "server-a"),
      grant("g3", "server-b"),
    ]);
    const changes = grantChanges(granted, [
      { resourceId: "server-b", projectId: "project-1" },
    ]);
    expect(changes.add).toEqual([]);
    expect(changes.remove).toEqual(["g1", "g2"]);
  });

  it("adds a server that has no grant yet", () => {
    const changes = grantChanges(grantedServers([grant("g1", "server-a")]), [
      { resourceId: "server-a", projectId: "project-1" },
      { resourceId: "server-c", projectId: "project-1" },
    ]);
    expect(changes.add).toEqual([
      { resourceId: "server-c", projectId: "project-1" },
    ]);
    expect(changes.remove).toEqual([]);
  });
});
