import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { describe, expect, it, vi } from "vitest";
import {
  discoverUnscopedGrants,
  hooksIngestKeyGrants,
  hooksIngestPolicyGrant,
} from "./agent-hook-events";

const bounded = hooksIngestPolicyGrant("org_example", "project_one");
const connect: AgentPolicyGrantForm = {
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId: "toolset_one" },
};

describe("hooksIngestPolicyGrant", () => {
  it("bounds hook ingestion to the agent's project", () => {
    expect(bounded).toEqual({
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

describe("discoverUnscopedGrants", () => {
  it("asks without naming a server", async () => {
    const listDelegableGrants = vi.fn().mockResolvedValue([connect, bounded]);

    const grants = await discoverUnscopedGrants(
      { listDelegableGrants },
      "agent_one",
      new AbortController().signal,
    );

    expect(listDelegableGrants.mock.calls[0]?.[0]).toEqual({
      agentId: "agent_one",
    });
    expect(grants).toEqual([connect, bounded]);
  });

  it("refuses an answer that is not a list", async () => {
    const listDelegableGrants = vi.fn().mockResolvedValue({});

    await expect(
      discoverUnscopedGrants(
        { listDelegableGrants },
        "agent_one",
        new AbortController().signal,
      ),
    ).rejects.toThrow("Invalid delegable permission response");
  });
});

describe("hooksIngestKeyGrants", () => {
  it("keeps only hook ingestion in the agent's project", () => {
    const elsewhere = {
      ...bounded,
      selector: { ...bounded.selector, projectId: "project_two" },
    };

    expect(
      hooksIngestKeyGrants([connect, bounded, elsewhere], "project_one"),
    ).toEqual([bounded]);
  });

  it("narrows a candidate with no project bound to the agent's project", () => {
    const unbounded = {
      ...bounded,
      selector: { resourceKind: "org" as const, resourceId: "org_example" },
    };
    const wildcard = {
      ...bounded,
      selector: { ...bounded.selector, projectId: "*" },
    };

    // Both narrow to the same grant, which is requested once.
    expect(
      hooksIngestKeyGrants([unbounded, wildcard, bounded], "project_one"),
    ).toEqual([bounded]);
  });
});
