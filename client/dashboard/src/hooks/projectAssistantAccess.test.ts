import { describe, expect, it } from "vitest";
import {
  isNoMcpAccessConfigured,
  showProjectAssistantConnecting,
} from "./projectAssistantAccess";

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
