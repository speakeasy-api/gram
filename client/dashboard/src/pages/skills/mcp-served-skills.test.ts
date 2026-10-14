import { describe, expect, it } from "vitest";
import {
  groupServedSkillsByPlugin,
  hostedPluginServers,
  pluginServerServesToolset,
  pluginServesToolset,
  servedSkillDigest,
  shortServedSkillDigest,
} from "./mcp-served-skills";

const toolsetId = "toolset-a";

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
});

describe("hostedPluginServers", () => {
  const servers = [
    { id: "s1", displayName: "Hosted by toolset", toolsetId },
    { id: "s2", displayName: "Hosted by wrapper", mcpServerId: "toolset-b" },
    { id: "s3", displayName: "Remote", mcpServerId: "remote-1" },
  ];

  it("keeps toolset-keyed servers without a toolset list", () => {
    expect(hostedPluginServers(servers).map((s) => s.id)).toEqual(["s1"]);
  });

  it("also keeps wrapper-keyed servers whose id names a toolset", () => {
    expect(
      hostedPluginServers(servers, new Set([toolsetId, "toolset-b"])).map(
        (s) => s.id,
      ),
    ).toEqual(["s1", "s2"]);
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
