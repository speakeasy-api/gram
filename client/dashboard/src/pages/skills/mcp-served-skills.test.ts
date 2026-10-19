import { describe, expect, it } from "vitest";
import {
  groupServedSkillsByPlugin,
  hostedPluginServers,
  hostedPluginServerToolsetId,
  hostedServerIndex,
  pluginServerServesToolset,
  pluginServesToolset,
  pluginsHaveUnresolvedServers,
  servedSkillDigest,
  shortServedSkillDigest,
} from "./mcp-served-skills";

const toolsetId = "toolset-a";

/** The project's MCP servers: the canonical wrapper, a second wrapper of the
 * same toolset, a wrapper of another toolset and a remote server. */
const mcpServers = [
  { id: toolsetId, toolsetId },
  { id: "wrapper-2", toolsetId },
  { id: "wrapper-b", toolsetId: "toolset-b" },
  { id: "remote-1" },
];

describe("hostedServerIndex", () => {
  it("maps every hosted server and toolset id to its toolset", () => {
    const index = hostedServerIndex(mcpServers, [{ id: "toolset-c" }]);
    expect([...index.entries()]).toEqual([
      ["toolset-c", "toolset-c"],
      [toolsetId, toolsetId],
      ["wrapper-2", toolsetId],
      ["wrapper-b", "toolset-b"],
      ["remote-1", null],
    ]);
  });

  it("marks a toolset with MCP off as serving nothing unless another wrapper fronts it", () => {
    const index = hostedServerIndex(
      [...mcpServers, { id: "toolset-c", toolsetId: "toolset-c" }],
      [
        { id: toolsetId, mcpEnabled: false },
        { id: "toolset-c", mcpEnabled: false },
      ],
    );
    // wrapper-2 still serves toolset-a, so its toolset stays reachable.
    expect(index.get(toolsetId)).toBe(toolsetId);
    expect(index.get("toolset-c")).toBeNull();
  });

  it("is empty without servers or toolsets", () => {
    expect(hostedServerIndex(undefined).size).toBe(0);
  });
});

describe("hostedPluginServerToolsetId", () => {
  const index = hostedServerIndex(mcpServers);

  it("resolves a wrapper-keyed server through the index", () => {
    expect(
      hostedPluginServerToolsetId({ mcpServerId: "wrapper-2" }, index),
    ).toBe(toolsetId);
  });

  it("has nothing for a listed server that serves no skills", () => {
    expect(
      hostedPluginServerToolsetId({ mcpServerId: "remote-1" }, index),
    ).toBeUndefined();
  });

  it("takes an unindexed id as a canonical wrapper", () => {
    expect(hostedPluginServerToolsetId({ mcpServerId: "toolset-z" })).toBe(
      "toolset-z",
    );
  });

  it("has nothing for a server without either key", () => {
    expect(hostedPluginServerToolsetId({})).toBeUndefined();
  });
});

describe("servedSkillDigest", () => {
  it("renders the sha256 form agents see over MCP, lowercased", () => {
    expect(servedSkillDigest("ABCDEF0123")).toBe("sha256:abcdef0123");
  });

  it("shortens the digest to a labeled prefix", () => {
    expect(shortServedSkillDigest("0123456789abcdef0123456789abcdef")).toBe(
      "sha256:0123456789ab…",
    );
  });
});

describe("pluginServerServesToolset", () => {
  it("matches a server keyed by the toolset id", () => {
    expect(pluginServerServesToolset({ toolsetId }, toolsetId)).toBe(true);
  });

  it("matches the hosted server's canonical mcp_servers id", () => {
    expect(
      pluginServerServesToolset({ mcpServerId: toolsetId }, toolsetId),
    ).toBe(true);
  });

  it("matches a non-canonical wrapper of the toolset through the index", () => {
    const index = hostedServerIndex(mcpServers);
    expect(
      pluginServerServesToolset({ mcpServerId: "wrapper-2" }, toolsetId, index),
    ).toBe(true);
    expect(
      pluginServerServesToolset({ mcpServerId: "wrapper-b" }, toolsetId, index),
    ).toBe(false);
    expect(
      pluginServerServesToolset({ mcpServerId: "wrapper-2" }, toolsetId),
    ).toBe(false);
  });

  it("ignores other servers and empty toolset ids", () => {
    expect(
      pluginServerServesToolset({ toolsetId: "toolset-b" }, toolsetId),
    ).toBe(false);
    expect(
      pluginServerServesToolset({ mcpServerId: "remote-1" }, toolsetId),
    ).toBe(false);
    expect(pluginServerServesToolset({ toolsetId }, "")).toBe(false);
  });
});

describe("pluginServesToolset", () => {
  it("is true when any included server is the hosted server", () => {
    expect(
      pluginServesToolset(
        {
          servers: [
            { id: "s1", displayName: "Remote", mcpServerId: "remote-1" },
            { id: "s2", displayName: "Hosted", toolsetId },
          ],
        },
        toolsetId,
      ),
    ).toBe(true);
  });

  it("is false for a plugin without servers", () => {
    expect(pluginServesToolset({}, toolsetId)).toBe(false);
    expect(pluginServesToolset({ servers: [] }, toolsetId)).toBe(false);
  });

  it("is true for a plugin including a non-canonical wrapper", () => {
    const plugin = {
      servers: [{ id: "s1", displayName: "Wrapper", mcpServerId: "wrapper-2" }],
    };
    expect(
      pluginServesToolset(plugin, toolsetId, hostedServerIndex(mcpServers)),
    ).toBe(true);
    expect(pluginServesToolset(plugin, toolsetId)).toBe(false);
  });
});

describe("hostedPluginServers", () => {
  const servers = [
    { id: "s1", displayName: "Hosted by toolset", toolsetId },
    { id: "s2", displayName: "Hosted by wrapper", mcpServerId: "toolset-b" },
    { id: "s3", displayName: "Remote", mcpServerId: "remote-1" },
  ];
  const ids = (list: { id: string }[]) => list.map((s) => s.id);

  it("keeps toolset-keyed servers and leaves the rest unresolved without an index", () => {
    const result = hostedPluginServers(servers);
    expect(ids(result.servers)).toEqual(["s1"]);
    expect(ids(result.unresolved)).toEqual(["s2", "s3"]);
  });

  it("also keeps wrapper-keyed servers the index resolves", () => {
    const byToolsets = hostedPluginServers(
      servers,
      hostedServerIndex(undefined, [{ id: toolsetId }, { id: "toolset-b" }]),
    );
    expect(ids(byToolsets.servers)).toEqual(["s1", "s2"]);
    expect(ids(byToolsets.unresolved)).toEqual(["s3"]);
    const byServers = hostedPluginServers(
      [
        ...servers,
        { id: "s4", displayName: "Wrapper", mcpServerId: "wrapper-2" },
      ],
      hostedServerIndex(mcpServers),
    );
    expect(ids(byServers.servers)).toEqual(["s1", "s4"]);
    expect(ids(byServers.unresolved)).toEqual(["s2"]);
  });

  it("drops a listed remote server rather than counting it as hidden", () => {
    const result = hostedPluginServers(
      [{ id: "s3", displayName: "Remote", mcpServerId: "remote-1" }],
      hostedServerIndex(mcpServers),
    );
    expect(result).toEqual({ servers: [], unresolved: [] });
  });

  it("drops a toolset-keyed server whose toolset has MCP off", () => {
    const result = hostedPluginServers(
      [{ id: "s6", displayName: "Off", toolsetId: "toolset-c" }],
      hostedServerIndex(undefined, [{ id: "toolset-c", mcpEnabled: false }]),
    );
    expect(result).toEqual({ servers: [], unresolved: [] });
  });

  it("does not take an unindexed id as remote", () => {
    // The caller may lack the grant to view a hosted wrapper, so the row is
    // reported as unresolved rather than dropped.
    const result = hostedPluginServers(
      [{ id: "s5", displayName: "Hidden wrapper", mcpServerId: "wrapper-9" }],
      hostedServerIndex(mcpServers),
    );
    expect(result.servers).toEqual([]);
    expect(ids(result.unresolved)).toEqual(["s5"]);
  });

  it("returns nothing for an undefined list", () => {
    expect(hostedPluginServers(undefined)).toEqual({
      servers: [],
      unresolved: [],
    });
  });
});

describe("pluginsHaveUnresolvedServers", () => {
  const index = hostedServerIndex(mcpServers);

  it("is true when any plugin includes a server the index cannot resolve", () => {
    expect(
      pluginsHaveUnresolvedServers(
        [
          { servers: [{ id: "s1", displayName: "Hosted", toolsetId }] },
          {
            servers: [
              { id: "s2", displayName: "Hidden", mcpServerId: "wrapper-9" },
            ],
          },
        ],
        index,
      ),
    ).toBe(true);
  });

  it("is false when every server resolves or carries a toolset id", () => {
    expect(
      pluginsHaveUnresolvedServers(
        [
          { servers: [{ id: "s1", displayName: "Hosted", toolsetId }] },
          {
            servers: [
              { id: "s2", displayName: "Wrapper", mcpServerId: "wrapper-2" },
            ],
          },
          {},
        ],
        index,
      ),
    ).toBe(false);
  });
});

describe("groupServedSkillsByPlugin", () => {
  it("groups distributions under serving plugins and drops the rest", () => {
    const groups = groupServedSkillsByPlugin(
      [
        { id: "d1", pluginId: "plugin-a" },
        { id: "d2", pluginId: "plugin-b" },
        { id: "d3", pluginId: "plugin-a" },
      ],
      [
        { id: "plugin-a", name: "A" },
        { id: "plugin-c", name: "C" },
      ],
    );
    expect(
      groups.map((g) => [g.key, g.plugin, g.items.map((d) => d.id)]),
    ).toEqual([
      ["plugin-a", { id: "plugin-a", name: "A" }, ["d1", "d3"]],
      ["plugin-c", { id: "plugin-c", name: "C" }, []],
    ]);
  });
});
