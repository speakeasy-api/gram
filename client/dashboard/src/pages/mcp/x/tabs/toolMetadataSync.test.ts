import type { ProxiedMcpTool } from "@/hooks/useProxiedMcpTools";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { describe, expect, it } from "vitest";
import { computeDrift, fullSyncBatch, newToolsBatch } from "./toolMetadataSync";
import type { ToolMetadataByName } from "./useToolMetadata";

function live(
  ...tools: Array<[string, ProxiedMcpTool["annotations"]]>
): Record<string, ProxiedMcpTool> {
  return Object.fromEntries(
    tools.map(([name, annotations]) => [name, { annotations }]),
  );
}

function stored(
  toolName: string,
  overrides: Partial<ToolMetadata> = {},
): ToolMetadata {
  return {
    mcpServerId: "server-1",
    toolName,
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

function byName(...entries: ToolMetadata[]): ToolMetadataByName {
  return Object.fromEntries(entries.map((e) => [e.toolName, e]));
}

describe("computeDrift", () => {
  it("reports nothing when the stored set already mirrors the session", () => {
    const drift = computeDrift(
      live(["alpha", { readOnlyHint: true }]),
      byName(stored("alpha", { readOnlyHint: true })),
    );
    expect(drift).toEqual([]);
  });

  it("flags a tool the session advertises but Speakeasy has never stored", () => {
    const drift = computeDrift(
      live(["alpha", { readOnlyHint: true }]),
      byName(),
    );
    expect(drift).toEqual([
      { kind: "new", toolName: "alpha", advertised: { readOnlyHint: true } },
    ]);
  });

  it("flags a tool the session stopped advertising", () => {
    const drift = computeDrift(live(), byName(stored("gone")));
    expect(drift).toEqual([{ kind: "removed", toolName: "gone" }]);
  });

  it("reports the fields that disagree, stored value first", () => {
    const drift = computeDrift(
      live(["alpha", { destructiveHint: true, title: "Alpha" }]),
      byName(stored("alpha", { destructiveHint: false })),
    );

    expect(drift).toEqual([
      {
        kind: "changed",
        toolName: "alpha",
        changes: [
          { field: "title", stored: undefined, advertised: "Alpha" },
          { field: "destructiveHint", stored: false, advertised: true },
        ],
      },
    ]);
  });

  it("treats an unset hint as different from an explicit false", () => {
    // The runtime distinguishes the two, so a sync has to be able to move a
    // stored `false` back to unset when the server stops asserting it.
    const drift = computeDrift(
      live(["alpha", {}]),
      byName(stored("alpha", { readOnlyHint: false })),
    );
    expect(drift).toEqual([
      {
        kind: "changed",
        toolName: "alpha",
        changes: [
          { field: "readOnlyHint", stored: false, advertised: undefined },
        ],
      },
    ]);
  });
});

describe("newToolsBatch", () => {
  it("is null when every advertised tool is already stored", () => {
    expect(
      newToolsBatch(live(["alpha", {}]), byName(stored("alpha"))),
    ).toBeNull();
  });

  it("sends only the tools with no stored entry", () => {
    // addToolMetadataBatch is a strict insert — naming a tool that already has
    // an entry fails the whole batch with a 409.
    const batch = newToolsBatch(
      live(
        ["alpha", { readOnlyHint: false }],
        ["beta", { readOnlyHint: true }],
      ),
      byName(stored("alpha", { readOnlyHint: true })),
    );

    expect(batch).toEqual([
      {
        toolName: "beta",
        title: undefined,
        readOnlyHint: true,
        destructiveHint: undefined,
        idempotentHint: undefined,
        openWorldHint: undefined,
      },
    ]);
  });

  it("never carries a stored tool the session no longer advertises", () => {
    // The additive pass cannot delete anything, and a stored entry is not ours
    // to resend — only an explicit sync touches those.
    const batch = newToolsBatch(
      live(["beta", {}]),
      byName(stored("vanished", { readOnlyHint: true })),
    );

    expect(batch?.map((f) => f.toolName)).toEqual(["beta"]);
  });
});

describe("fullSyncBatch", () => {
  it("sends only live tools, so stored-but-unadvertised ones remove", () => {
    const batch = fullSyncBatch(
      live(["alpha", { readOnlyHint: true, title: "Alpha" }]),
    );

    expect(batch).toEqual([
      {
        toolName: "alpha",
        title: "Alpha",
        readOnlyHint: true,
        destructiveHint: undefined,
        idempotentHint: undefined,
        openWorldHint: undefined,
      },
    ]);
  });

  it("is empty when the session advertises nothing, removing the whole set", () => {
    expect(fullSyncBatch(live())).toEqual([]);
  });
});
