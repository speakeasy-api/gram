import { describe, expect, it } from "vitest";
import {
  groupServedSkillsByPlugin,
  hostedPluginServers,
  hostedPluginServerToolsetId,
  hostedServerIndex,
  pluginServerServesToolset,
  pluginServesToolset,
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
    const index = hostedServerIndex(mcpServers, ["toolset-c"]);
    expect([...index.entries()]).toEqual([
      ["toolset-c", "toolset-c"],
      [toolsetId, toolsetId],
      ["wrapper-2", toolsetId],
      ["wrapper-b", "toolset-b"],
    ]);
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

  it("keeps toolset-keyed servers without an index", () => {
    expect(hostedPluginServers(servers).map((s) => s.id)).toEqual(["s1"]);
  });

  it("also keeps wrapper-keyed servers the index resolves", () => {
    expect(
      hostedPluginServers(
        servers,
        hostedServerIndex(undefined, [toolsetId, "toolset-b"]),
      ).map((s) => s.id),
    ).toEqual(["s1", "s2"]);
    expect(
      hostedPluginServers(
        [
          ...servers,
          { id: "s4", displayName: "Wrapper", mcpServerId: "wrapper-2" },
        ],
        hostedServerIndex(mcpServers),
      ).map((s) => s.id),
    ).toEqual(["s1", "s4"]);
  });

  it("returns nothing for an undefined list", () => {
    expect(hostedPluginServers(undefined)).toEqual([]);
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
    expect(groups.map((g) => [g.key, g.items.map((d) => d.id)])).toEqual([
      ["plugin-a", ["d1", "d3"]],
      ["plugin-c", []],
    ]);
  });
});
