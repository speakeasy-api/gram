import { describe, expect, it } from "vitest";
import {
  isNoMcpAccessConfigured,
  settledListCount,
  showProjectAssistantConnecting,
} from "./projectAssistantAccess";

describe("settledListCount", () => {
  it("stays unknown when the query has no data", () => {
    expect(settledListCount(undefined, undefined)).toBeUndefined();
    expect(settledListCount(undefined, [])).toBeUndefined();
  });

  it("does not treat a settled empty list as unknown", () => {
    expect(settledListCount({ mcpServers: [] }, [])).toBe(0);
    expect(settledListCount({ mcpServers: undefined }, undefined)).toBe(0);
  });

  it("counts a settled list", () => {
    expect(settledListCount({ mcpServers: [{ id: "1" }] }, [{ id: "1" }])).toBe(
      1,
    );
  });
});

describe("isNoMcpAccessConfigured", () => {
  const settledEmpty = {
    projectSlug: "proj",
    toolsetsLoading: false,
    toolsetCount: 0,
    mcpServersLoading: false,
    mcpServerCount: 0,
  };

  it("is false while the project slug is missing or either query is loading", () => {
    expect(isNoMcpAccessConfigured({ ...settledEmpty, projectSlug: "" })).toBe(
      false,
    );
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, toolsetsLoading: true }),
    ).toBe(false);
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, mcpServersLoading: true }),
    ).toBe(false);
  });

  it("is true only when both toolsets and MCP servers settled empty", () => {
    expect(isNoMcpAccessConfigured(settledEmpty)).toBe(true);
  });

  it("is false when the project has toolsets", () => {
    expect(isNoMcpAccessConfigured({ ...settledEmpty, toolsetCount: 2 })).toBe(
      false,
    );
  });

  it("is false when the project has MCP servers but no toolsets", () => {
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, mcpServerCount: 1 }),
    ).toBe(false);
  });

  it("is false when either listing failed or the count is still unknown", () => {
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, toolsetsFailed: true }),
    ).toBe(false);
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, mcpServersFailed: true }),
    ).toBe(false);
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, toolsetCount: undefined }),
    ).toBe(false);
    expect(
      isNoMcpAccessConfigured({ ...settledEmpty, mcpServerCount: undefined }),
    ).toBe(false);
  });
});

describe("showProjectAssistantConnecting", () => {
  it("shows the spinner only while connecting with servers available", () => {
    expect(
      showProjectAssistantConnecting({
        assistantError: undefined,
        assistantNeedsAdmin: false,
        noMcpAccessConfigured: false,
      }),
    ).toBe(true);
  });

  it("does not claim to be connecting when no servers are configured", () => {
    expect(
      showProjectAssistantConnecting({
        assistantError: undefined,
        assistantNeedsAdmin: false,
        noMcpAccessConfigured: true,
      }),
    ).toBe(false);
  });

  it("hides the spinner when the assistant failed or needs an admin", () => {
    expect(
      showProjectAssistantConnecting({
        assistantError: "failed",
        assistantNeedsAdmin: false,
        noMcpAccessConfigured: false,
      }),
    ).toBe(false);
    expect(
      showProjectAssistantConnecting({
        assistantError: undefined,
        assistantNeedsAdmin: true,
        noMcpAccessConfigured: false,
      }),
    ).toBe(false);
  });
});
