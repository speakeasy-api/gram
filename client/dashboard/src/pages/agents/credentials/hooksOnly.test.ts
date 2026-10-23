import { describe, expect, it, vi } from "vitest";
import {
  discoverHooksOnlyGrants,
  hooksOnlyPolicyGrants,
  missingHooksOnlyScopes,
} from "./hooksOnly";

const [hooksIngest, deviceSync] = hooksOnlyPolicyGrants(
  "org_example",
  "project_one",
);
const connect = {
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId: "toolset_one" },
};

describe("hooksOnlyPolicyGrants", () => {
  it("bounds hook ingestion to the project and leaves plugin sync unbounded", () => {
    expect(hooksIngest?.selector).toEqual({
      resourceKind: "org",
      resourceId: "org_example",
      projectId: "project_one",
    });
    // The server rejects a project bound on plugin sync.
    expect(deviceSync?.selector).toEqual({
      resourceKind: "org",
      resourceId: "org_example",
    });
  });
});

describe("discoverHooksOnlyGrants", () => {
  it("asks without naming a server and keeps only the hook grants", async () => {
    const listDelegableGrants = vi
      .fn()
      .mockResolvedValue([connect, hooksIngest, deviceSync]);

    const grants = await discoverHooksOnlyGrants(
      { listDelegableGrants },
      "agent_one",
      new AbortController().signal,
    );

    expect(listDelegableGrants.mock.calls[0]?.[0]).toEqual({
      agentId: "agent_one",
    });
    expect(grants).toEqual([hooksIngest, deviceSync]);
  });

  it("refuses an answer that is not a list", async () => {
    const listDelegableGrants = vi.fn().mockResolvedValue({});

    await expect(
      discoverHooksOnlyGrants(
        { listDelegableGrants },
        "agent_one",
        new AbortController().signal,
      ),
    ).rejects.toThrow("Invalid delegable permission response");
  });
});

describe("missingHooksOnlyScopes", () => {
  it("names each hook grant discovery did not return", () => {
    expect(missingHooksOnlyScopes([])).toEqual([
      "org:hooks_ingest",
      "org:device_agent_sync",
    ]);
    expect(missingHooksOnlyScopes([hooksIngest!])).toEqual([
      "org:device_agent_sync",
    ]);
    expect(missingHooksOnlyScopes([hooksIngest!, deviceSync!])).toEqual([]);
  });
});
