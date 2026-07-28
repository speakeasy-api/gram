import { describe, it, expect } from "vitest";
import {
  grantResourceIdForMcpServer,
  mcpServerDisplayName,
  mergeMcpServersIntoGroups,
  type McpServerRow,
  type Server,
  type ServerGroup,
} from "./serverMerge";

// --- Helpers ---

const toolsetServer = (id: string, name = `toolset ${id}`): Server => ({
  id,
  name,
  slug: `host/mcp/${id}`,
  mcpSlug: id,
  tools: [{ id: `${id}-tool`, name: "do_thing", type: "http" }],
  dynamicTools: false,
});

const group = (projectId: string, servers: Server[]): ServerGroup => ({
  projectId,
  projectName: `Project ${projectId}`,
  servers,
});

const row = (
  overrides: Partial<McpServerRow> & { id: string },
): McpServerRow => ({
  projectId: "p1",
  name: undefined,
  slug: undefined,
  toolsetId: undefined,
  ...overrides,
});

const projectNames = new Map([
  ["p1", "Project p1"],
  ["p2", "Project p2"],
]);

// --- grantResourceIdForMcpServer ---
// This mapping is the load-bearing invariant: enforcement checks the toolset
// id for toolset-backed serving and the mcp_servers id for remote/tunneled.

describe("grantResourceIdForMcpServer", () => {
  it("uses the mcp_servers row id for remote/tunneled backends", () => {
    expect(grantResourceIdForMcpServer(row({ id: "srv-1" }))).toBe("srv-1");
  });

  it("uses the toolset id for toolset-backed rows", () => {
    expect(
      grantResourceIdForMcpServer(row({ id: "srv-1", toolsetId: "ts-1" })),
    ).toBe("ts-1");
  });
});

// --- mcpServerDisplayName ---

describe("mcpServerDisplayName", () => {
  it("prefers name, then slug, then id", () => {
    expect(
      mcpServerDisplayName(row({ id: "srv-1", name: "My Server", slug: "my" })),
    ).toBe("My Server");
    expect(mcpServerDisplayName(row({ id: "srv-1", slug: "my" }))).toBe("my");
    expect(mcpServerDisplayName(row({ id: "srv-1" }))).toBe("srv-1");
  });
});

// --- mergeMcpServersIntoGroups ---

describe("mergeMcpServersIntoGroups", () => {
  it("adds remote/tunneled rows with dynamicTools and their own id", () => {
    const groups = [group("p1", [toolsetServer("ts-1")])];
    const merged = mergeMcpServersIntoGroups(
      groups,
      [row({ id: "srv-1", name: "Remote One" })],
      projectNames,
    );
    expect(merged).toHaveLength(1);
    const servers = merged[0]!.servers;
    expect(servers.map((s) => s.id)).toEqual(["ts-1", "srv-1"]);
    expect(servers[1]).toMatchObject({
      id: "srv-1",
      name: "Remote One",
      dynamicTools: true,
      tools: [],
    });
  });

  it("dedupes toolset-backed rows against the existing toolset entry", () => {
    const existing = toolsetServer("ts-1");
    const merged = mergeMcpServersIntoGroups(
      [group("p1", [existing])],
      [row({ id: "srv-1", name: "shadowed", toolsetId: "ts-1" })],
      projectNames,
    );
    expect(merged[0]!.servers).toHaveLength(1);
    // The toolset entry wins: it carries the enumerable tools.
    expect(merged[0]!.servers[0]).toEqual(existing);
  });

  it("adds toolset-backed rows whose toolset entry was filtered out", () => {
    const merged = mergeMcpServersIntoGroups(
      [group("p1", [])],
      [row({ id: "srv-1", name: "Zero Tools", toolsetId: "ts-9" })],
      projectNames,
    );
    expect(merged[0]!.servers).toEqual([
      {
        id: "ts-9",
        name: "Zero Tools",
        slug: "",
        mcpSlug: undefined,
        tools: [],
        dynamicTools: false,
      },
    ]);
  });

  it("collapses rows sharing a toolset id to one entry", () => {
    const merged = mergeMcpServersIntoGroups(
      [],
      [
        row({ id: "srv-1", name: "first", toolsetId: "ts-1" }),
        row({ id: "srv-2", name: "second", toolsetId: "ts-1" }),
      ],
      projectNames,
    );
    expect(merged[0]!.servers.map((s) => s.id)).toEqual(["ts-1"]);
    expect(merged[0]!.servers[0]!.name).toBe("first");
  });

  it("creates a group for projects with no toolset entries", () => {
    const merged = mergeMcpServersIntoGroups(
      [group("p1", [toolsetServer("ts-1")])],
      [row({ id: "srv-1", projectId: "p2", name: "Other Project" })],
      projectNames,
    );
    expect(merged.map((g) => g.projectId)).toEqual(["p1", "p2"]);
    expect(merged[1]!.projectName).toBe("Project p2");
  });

  it("falls back to Unknown for projects missing from the name map", () => {
    const merged = mergeMcpServersIntoGroups(
      [],
      [row({ id: "srv-1", projectId: "p-mystery" })],
      projectNames,
    );
    expect(merged[0]!.projectName).toBe("Unknown");
  });

  it("drops groups left with no servers", () => {
    const merged = mergeMcpServersIntoGroups(
      [group("p1", [])],
      [],
      projectNames,
    );
    expect(merged).toEqual([]);
  });

  it("does not mutate the input groups", () => {
    const groups = [group("p1", [toolsetServer("ts-1")])];
    mergeMcpServersIntoGroups(
      groups,
      [row({ id: "srv-1" }), row({ id: "srv-2", projectId: "p2" })],
      projectNames,
    );
    expect(groups).toEqual([group("p1", [toolsetServer("ts-1")])]);
  });
});
