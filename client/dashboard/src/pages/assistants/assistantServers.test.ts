import { describe, expect, it } from "vitest";
import { assistantAttachedServerSlugs } from "./assistantServers";

describe("assistantAttachedServerSlugs", () => {
  it("is empty when the assistant has neither toolsets nor direct MCP servers", () => {
    expect(
      assistantAttachedServerSlugs({ toolsets: [], mcpServers: [] }),
    ).toEqual([]);
  });

  it("includes toolset slugs", () => {
    expect(
      assistantAttachedServerSlugs({
        toolsets: [{ toolsetSlug: "github" }, { toolsetSlug: "linear" }],
        mcpServers: [],
      }),
    ).toEqual(["github", "linear"]);
  });

  it("includes directly attached MCP servers when there are no toolsets", () => {
    expect(
      assistantAttachedServerSlugs({
        toolsets: [],
        mcpServers: [{ mcpServerSlug: "remote-docs" }],
      }),
    ).toEqual(["remote-docs"]);
  });

  it("lists toolsets and directly attached MCP servers together", () => {
    expect(
      assistantAttachedServerSlugs({
        toolsets: [{ toolsetSlug: "github" }],
        mcpServers: [{ mcpServerSlug: "remote-docs" }],
      }),
    ).toEqual(["github", "remote-docs"]);
  });

  it("dedupes overlapping toolset and MCP server slugs", () => {
    expect(
      assistantAttachedServerSlugs({
        toolsets: [{ toolsetSlug: "github" }],
        mcpServers: [{ mcpServerSlug: "github" }],
      }),
    ).toEqual(["github"]);
  });

  it("treats a missing mcpServers array as empty", () => {
    expect(
      assistantAttachedServerSlugs({
        toolsets: [{ toolsetSlug: "github" }],
      }),
    ).toEqual(["github"]);
  });
});
