import { describe, it, expect } from "vitest";
import {
  computePanelState,
  getSelectedCollectionCount,
  type CollectionGroup,
} from "./computePanelState";
import type { Selector } from "./types";

// --- Helpers ---

const tool = (resourceId: string, toolName: string): Selector => ({
  resourceKind: "mcp",
  resourceId,
  tool: toolName,
});

const server = (resourceId: string): Selector => ({
  resourceKind: "mcp",
  resourceId,
});

const disposition = (d: string): Selector => ({
  resourceKind: "mcp",
  resourceId: "*",
  disposition: d as Selector["disposition"],
});

// --- Fixtures ---

const collections: CollectionGroup[] = [
  {
    id: "col-1",
    name: "Backend Tools",
    servers: [
      {
        id: "srv-a",
        tools: [{ name: "create-user" }, { name: "delete-user" }],
      },
      { id: "srv-b", tools: [{ name: "send-email" }] },
    ],
  },
  {
    id: "col-2",
    name: "Frontend Tools",
    servers: [
      { id: "srv-c", tools: [{ name: "deploy" }, { name: "preview" }] },
    ],
  },
];

// --- Tests ---

describe("computePanelState", () => {
  describe("all panel", () => {
    it("null selectors → all panel with correct label", () => {
      const result = computePanelState(null, [], "mcp");
      expect(result).toEqual({ activePanel: "all", label: "All servers" });
    });

    it("null selectors with project resourceType", () => {
      const result = computePanelState(null, [], "project");
      expect(result).toEqual({ activePanel: "all", label: "All projects" });
    });

    it("ignores collection data when null", () => {
      const result = computePanelState(null, collections, "mcp");
      expect(result.activePanel).toBe("all");
    });
  });

  describe("servers panel", () => {
    it("empty selectors → servers with Select... label", () => {
      const result = computePanelState([], [], "mcp");
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: [],
        label: "Select...",
      });
    });

    it("server-level selectors → servers panel with IDs", () => {
      const result = computePanelState(
        [server("srv-a"), server("srv-b")],
        collections,
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["srv-a", "srv-b"],
        label: "2 servers selected",
      });
    });

    it("single server → singular label", () => {
      const result = computePanelState([server("srv-a")], [], "mcp");
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["srv-a"],
        label: "1 server selected",
      });
    });

    it("project resource type uses 'project' noun", () => {
      const result = computePanelState(
        [server("proj-1"), server("proj-2")],
        [],
        "project",
      );
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["proj-1", "proj-2"],
        label: "2 projects selected",
      });
    });
  });

  describe("tools panel — select tab", () => {
    it("tool selectors with no collection match → tools/select", () => {
      const result = computePanelState(
        [tool("srv-a", "create-user")], // partial — doesn't complete a collection
        collections,
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "tools",
        tab: "select",
        annotations: [],
        selectedTools: [{ serverId: "srv-a", tool: "create-user" }],
        label: "1 tool selected",
      });
    });

    it("multiple tool selectors without collection match", () => {
      const result = computePanelState(
        [tool("srv-x", "foo"), tool("srv-x", "bar"), tool("srv-y", "baz")],
        collections,
        "mcp",
      );
      expect(result.activePanel).toBe("tools");
      if (result.activePanel === "tools" && result.tab === "select") {
        expect(result.selectedTools).toHaveLength(3);
        expect(result.label).toBe("3 tools selected");
      }
    });

    it("tool selectors with no collection data → tools", () => {
      const result = computePanelState(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
        ],
        [], // no collection data available
        "mcp",
      );
      expect(result.activePanel).toBe("tools");
    });
  });

  describe("tools panel — auto-groups tab", () => {
    it("disposition selectors → tools/auto-groups", () => {
      const result = computePanelState(
        [disposition("read_only"), disposition("idempotent")],
        collections,
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "tools",
        tab: "auto-groups",
        annotations: ["readOnlyHint", "idempotentHint"],
        label: "2 rules selected",
      });
    });

    it("single disposition → singular label", () => {
      const result = computePanelState([disposition("destructive")], [], "mcp");
      expect(result).toEqual({
        activePanel: "tools",
        tab: "auto-groups",
        annotations: ["destructiveHint"],
        label: "1 rule selected",
      });
    });
  });

  describe("collection panel", () => {
    it("selectors matching one full collection → collection panel", () => {
      const result = computePanelState(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
        ],
        collections,
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "collection",
        selectedCollectionCount: 1,
        label: "1 collection selected",
      });
    });

    it("selectors matching both collections → correct count", () => {
      const result = computePanelState(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
          tool("srv-c", "deploy"),
          tool("srv-c", "preview"),
        ],
        collections,
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "collection",
        selectedCollectionCount: 2,
        label: "2 collections selected",
      });
    });

    it("selectors matching one collection plus extra tools → still collection", () => {
      const result = computePanelState(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
          tool("srv-z", "unrelated"),
        ],
        collections,
        "mcp",
      );
      // At least one collection fully matched
      expect(result.activePanel).toBe("collection");
      if (result.activePanel === "collection") {
        expect(result.selectedCollectionCount).toBe(1);
      }
    });
  });
});

describe("getSelectedCollectionCount", () => {
  it("returns 0 for empty selectors", () => {
    expect(getSelectedCollectionCount([], collections)).toBe(0);
  });

  it("returns 0 for partial collection match", () => {
    expect(
      getSelectedCollectionCount([tool("srv-a", "create-user")], collections),
    ).toBe(0);
  });

  it("returns 1 for one full collection", () => {
    expect(
      getSelectedCollectionCount(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
        ],
        collections,
      ),
    ).toBe(1);
  });

  it("returns 2 for both collections", () => {
    expect(
      getSelectedCollectionCount(
        [
          tool("srv-a", "create-user"),
          tool("srv-a", "delete-user"),
          tool("srv-b", "send-email"),
          tool("srv-c", "deploy"),
          tool("srv-c", "preview"),
        ],
        collections,
      ),
    ).toBe(2);
  });

  it("skips collections with empty tool sets", () => {
    const emptyCollection: CollectionGroup[] = [
      { id: "empty", name: "Empty", servers: [{ id: "x", tools: [] }] },
    ];
    expect(
      getSelectedCollectionCount([tool("x", "anything")], emptyCollection),
    ).toBe(0);
  });
});
