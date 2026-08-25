import { describe, expect, it } from "vitest";
import {
  mcpToolsAvailability,
  mcpToolsSendBlocked,
  mcpToolsSendTooltip,
  mcpToolsWelcomeSubtitle,
  NO_MCP_TOOLS_MESSAGE,
} from "./mcpToolsAvailability";

describe("mcpToolsAvailability", () => {
  it("stays loading even when tools and error are empty", () => {
    expect(mcpToolsAvailability(true, undefined, null)).toBe("loading");
    expect(mcpToolsAvailability(true, {}, new Error("401"))).toBe("loading");
  });

  it("is unavailable when settled with an error", () => {
    expect(mcpToolsAvailability(false, undefined, new Error("401"))).toBe(
      "unavailable",
    );
  });

  it("stays loading when the query has not run yet", () => {
    expect(mcpToolsAvailability(false, undefined, null)).toBe("loading");
  });

  it("is unavailable when settled with zero tools", () => {
    expect(mcpToolsAvailability(false, {}, null)).toBe("unavailable");
  });

  it("is ready only when settled with at least one tool", () => {
    expect(mcpToolsAvailability(false, { list_issues: {} }, null)).toBe(
      "ready",
    );
  });

  it("does not claim access while loading or when empty", () => {
    expect(mcpToolsWelcomeSubtitle("loading", READY_SUBTITLE)).toBe(
      "Loading tools from this server…",
    );
    expect(mcpToolsWelcomeSubtitle("unavailable", READY_SUBTITLE)).toBe(
      NO_MCP_TOOLS_MESSAGE,
    );
    expect(mcpToolsWelcomeSubtitle("ready", READY_SUBTITLE)).toBe(
      READY_SUBTITLE,
    );
  });

  it("explains a blocked send", () => {
    expect(mcpToolsSendTooltip("loading")).toBe("Loading tools…");
    expect(mcpToolsSendTooltip("unavailable")).toBe(NO_MCP_TOOLS_MESSAGE);
    expect(mcpToolsSendTooltip("ready")).toBe("Send message");
  });

  it("blocks send only when the host requires tools and they are not ready", () => {
    expect(mcpToolsSendBlocked(undefined, false, {}, null)).toBe(false);
    expect(mcpToolsSendBlocked(true, true, undefined, null)).toBe(true);
    expect(mcpToolsSendBlocked(true, false, {}, new Error("401"))).toBe(true);
    expect(mcpToolsSendBlocked(true, false, { list_issues: {} }, null)).toBe(
      false,
    );
  });
});

const READY_SUBTITLE =
  "This chat has access to the selected MCP server. Use it to test your tools.";
