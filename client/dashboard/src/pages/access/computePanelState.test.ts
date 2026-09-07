import { describe, it, expect } from "vitest";
import { computePanelState } from "./computePanelState";
import type { Selector } from "@gram/client/models/components/selector.js";

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

// --- Tests ---

describe("computePanelState", () => {
  describe("all panel", () => {
    it("null selectors → all panel with correct label", () => {
      const result = computePanelState(null, "mcp");
      expect(result).toEqual({ activePanel: "all", label: "All servers" });
    });

    it("null selectors with project resourceType", () => {
      const result = computePanelState(null, "project");
      expect(result).toEqual({ activePanel: "all", label: "All projects" });
    });

    it("null selectors with skill resourceType", () => {
      const result = computePanelState(null, "skill");
      expect(result).toEqual({ activePanel: "all", label: "All projects" });
    });
  });

  describe("servers panel", () => {
    it("empty selectors → servers with Select... label", () => {
      const result = computePanelState([], "mcp");
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: [],
        label: "Select...",
      });
    });

    it("server-level selectors → servers panel with IDs", () => {
      const result = computePanelState(
        [server("srv-a"), server("srv-b")],
        "mcp",
      );
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["srv-a", "srv-b"],
        label: "2 servers selected",
      });
    });

    it("single server → singular label", () => {
      const result = computePanelState([server("srv-a")], "mcp");
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["srv-a"],
        label: "1 server selected",
      });
    });

    it("project resource type uses 'project' noun", () => {
      const result = computePanelState(
        [server("proj-1"), server("proj-2")],
        "project",
      );
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["proj-1", "proj-2"],
        label: "2 projects selected",
      });
    });

    it("skill resource type selects projects", () => {
      const result = computePanelState(
        [
          { resourceKind: "skill", resourceId: "proj-1" },
          { resourceKind: "skill", resourceId: "proj-2" },
        ],
        "skill",
      );
      expect(result).toEqual({
        activePanel: "servers",
        selectedServerIds: ["proj-1", "proj-2"],
        label: "2 projects selected",
      });
    });
  });

  describe("projects panel", () => {
    it("project selectors → projects panel with IDs", () => {
      const selectors: Selector[] = [
        { resourceKind: "mcp", resourceId: "*", projectId: "proj-1" },
        { resourceKind: "mcp", resourceId: "*", projectId: "proj-2" },
      ];
      const result = computePanelState(selectors, "mcp");
      expect(result).toEqual({
        activePanel: "projects",
        selectedProjectIds: ["proj-1", "proj-2"],
        label: "2 projects selected",
      });
    });

    it("single project → singular label", () => {
      const selectors: Selector[] = [
        { resourceKind: "mcp", resourceId: "*", projectId: "proj-1" },
      ];
      const result = computePanelState(selectors, "mcp");
      expect(result).toEqual({
        activePanel: "projects",
        selectedProjectIds: ["proj-1"],
        label: "1 project selected",
      });
    });
  });

  describe("tools panel — select tab", () => {
    it("tool selectors → tools/select", () => {
      const result = computePanelState([tool("srv-a", "create-user")], "mcp");
      expect(result).toEqual({
        activePanel: "tools",
        tab: "select",
        annotations: [],
        selectedTools: [{ serverId: "srv-a", tool: "create-user" }],
        label: "1 tool selected",
      });
    });
  });

  describe("tools panel — auto-groups tab", () => {
    it("disposition selectors → tools/auto-groups", () => {
      const result = computePanelState(
        [disposition("read_only"), disposition("idempotent")],
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
      const result = computePanelState([disposition("destructive")], "mcp");
      expect(result).toEqual({
        activePanel: "tools",
        tab: "auto-groups",
        annotations: ["destructiveHint"],
        label: "1 rule selected",
      });
    });
  });
});
