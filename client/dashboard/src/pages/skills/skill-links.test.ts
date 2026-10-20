import { describe, expect, it } from "vitest";
import {
  skillLinksFilter,
  skillLinkTargetFields,
  skillLinkTargetKey,
  skillLinkTargetName,
  sortSkillLinks,
} from "./skill-links";

describe("skillLinkTargetFields", () => {
  it("names exactly one surface per kind", () => {
    expect(
      skillLinkTargetFields({ kind: "mcp_server", mcpServerId: "server-a" }),
    ).toEqual({ targetKind: "mcp_server", mcpServerId: "server-a" });
    expect(
      skillLinkTargetFields({
        kind: "meta_mcp_server",
        metaMcpServerId: "gateway-a",
      }),
    ).toEqual({ targetKind: "meta_mcp_server", metaMcpServerId: "gateway-a" });
    expect(skillLinkTargetFields({ kind: "organization_gateway" })).toEqual({
      targetKind: "organization_gateway",
    });
  });
});

describe("skillLinksFilter", () => {
  it("filters to the one server or gateway", () => {
    expect(
      skillLinksFilter({ kind: "mcp_server", mcpServerId: "server-a" }),
    ).toEqual({ targetKind: "mcp_server", mcpServerId: "server-a" });
    expect(
      skillLinksFilter({ kind: "meta_mcp_server", metaMcpServerId: "gw" }),
    ).toEqual({ targetKind: "meta_mcp_server", metaMcpServerId: "gw" });
  });

  it("filters the organization gateway by kind alone", () => {
    expect(skillLinksFilter({ kind: "organization_gateway" })).toEqual({
      targetKind: "organization_gateway",
    });
  });
});

describe("skillLinkTargetKey", () => {
  it("differs per target and is stable per value", () => {
    expect(skillLinkTargetKey({ kind: "mcp_server", mcpServerId: "a" })).toBe(
      skillLinkTargetKey({ kind: "mcp_server", mcpServerId: "a" }),
    );
    expect(
      skillLinkTargetKey({ kind: "mcp_server", mcpServerId: "a" }),
    ).not.toBe(
      skillLinkTargetKey({ kind: "meta_mcp_server", metaMcpServerId: "a" }),
    );
  });
});

describe("skillLinkTargetName", () => {
  it("names the organization gateway and falls back for unnamed targets", () => {
    expect(skillLinkTargetName({ targetKind: "organization_gateway" })).toBe(
      "Organization gateway",
    );
    expect(skillLinkTargetName({ targetKind: "mcp_server" })).toBe(
      "Unnamed MCP server",
    );
    expect(
      skillLinkTargetName({ targetKind: "meta_mcp_server", targetName: "Ops" }),
    ).toBe("Ops");
  });
});

describe("sortSkillLinks", () => {
  it("orders by skill, then by target, without mutating the input", () => {
    const links = [
      { skillDisplayName: "Beta", targetKind: "mcp_server", targetName: "B" },
      { skillDisplayName: "Alpha", targetKind: "mcp_server", targetName: "Z" },
      { skillDisplayName: "Alpha", targetKind: "mcp_server", targetName: "A" },
    ] as const;
    const sorted = sortSkillLinks(links);
    expect(
      sorted.map((link) => `${link.skillDisplayName}/${link.targetName}`),
    ).toEqual(["Alpha/A", "Alpha/Z", "Beta/B"]);
    expect(links[0].skillDisplayName).toBe("Beta");
  });
});
