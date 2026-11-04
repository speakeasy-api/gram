import { describe, expect, it } from "vitest";
import {
  hooksIngestKeyGrants,
  hooksIngestPolicyGrant,
} from "./agent-hook-events";

describe("hooksIngestPolicyGrant", () => {
  it("bounds the grant to the agent's project", () => {
    expect(hooksIngestPolicyGrant("org_example", "project_one")).toEqual({
      effect: "allow",
      scope: "org:hooks_ingest",
      selector: {
        resourceKind: "org",
        resourceId: "org_example",
        projectId: "project_one",
      },
    });
  });
});

describe("hooksIngestKeyGrants", () => {
  const org = { resourceKind: "org" as const, resourceId: "org_example" };
  const hooks = (projectId?: string) => ({
    effect: "allow" as const,
    scope: "org:hooks_ingest",
    selector: projectId ? { ...org, projectId } : org,
  });

  it("narrows an unbounded or wildcard grant to the agent's project", () => {
    expect(hooksIngestKeyGrants([hooks(), hooks("*")], "project_one")).toEqual([
      hooks("project_one"),
    ]);
  });

  it("leaves out other scopes and grants bound to another project", () => {
    const connect = {
      effect: "allow" as const,
      scope: "mcp:connect",
      selector: { resourceKind: "mcp" as const, resourceId: "toolset_one" },
    };
    expect(
      hooksIngestKeyGrants([connect, hooks("project_two")], "project_one"),
    ).toEqual([]);
  });
});
