import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import { describe, expect, it } from "vitest";

import {
  CONNECT_SCOPE,
  grantChanges,
  grantedServers,
  wildcardConnectGrant,
} from "./agentPolicyEdit";

function grant(
  id: string,
  resourceId: string,
  // "" leaves the selector unscoped, the way an organization-wide grant comes
  // back from the server.
  projectId = "project-1",
): AgentPolicyGrant {
  return {
    id,
    agentId: "agent-1",
    effect: "allow",
    scope: CONNECT_SCOPE,
    selector: {
      resourceKind: "mcp",
      resourceId,
      ...(projectId ? { projectId } : {}),
    },
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

describe("wildcardConnectGrant", () => {
  it("reports a wildcard the selection cannot show", () => {
    expect(wildcardConnectGrant([grant("g1", "*", "")])).toEqual({
      projectIds: [],
      grantIds: ["g1"],
    });
    expect(wildcardConnectGrant([grant("g1", "server-a")])).toBeUndefined();
  });

  it("names the project a scoped wildcard is confined to", () => {
    expect(wildcardConnectGrant([grant("g1", "*", "project-1")])).toEqual({
      projectIds: ["project-1"],
      grantIds: ["g1"],
    });
  });

  it("keeps every project a scoped wildcard is held for", () => {
    // Reporting only the first would list one project's servers and leave a
    // key silently short of the other's.
    expect(
      wildcardConnectGrant([
        grant("g1", "*", "project-1"),
        grant("g2", "*", "project-2"),
      ]),
    ).toEqual({
      projectIds: ["project-1", "project-2"],
      grantIds: ["g1", "g2"],
    });
  });

  it("reports the org-wide reach when both kinds are granted", () => {
    expect(
      wildcardConnectGrant([
        grant("g1", "*", "project-1"),
        grant("g2", "*", ""),
      ]),
      // Both rows, because removing only the org-wide one would leave the
      // project's servers still reachable through the scoped one.
    ).toEqual({ projectIds: [], grantIds: ["g1", "g2"] });
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
