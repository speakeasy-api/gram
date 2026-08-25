import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { NO_MCP_TOOLS_MESSAGE } from "@/elements/lib/mcpToolsAvailability";

const mocks = vi.hoisted(() => ({
  ctx: {
    config: {
      welcome: {
        title: "Test Your MCP Server",
        subtitle:
          "This chat has access to the selected MCP server. Use it to test your tools.",
        suggestions: [
          {
            title: "Explore tools",
            label: "See what's available",
            prompt: "What tools does this server have?",
          },
        ],
      },
    },
    mcpTools: undefined as Record<string, unknown> | undefined,
    mcpToolsLoading: false,
    mcpToolsError: null as Error | null,
  },
}));

vi.mock("@/elements", () => ({
  useGramElements: () => mocks.ctx,
}));

vi.mock("@assistant-ui/react", () => ({
  ThreadPrimitive: {
    Suggestion: ({ children }: { children: React.ReactNode }) => children,
  },
}));

import {
  GramThreadWelcome,
  PlaygroundNoToolsBanner,
} from "./PlaygroundElementsOverrides";

const ACCESS_CLAIM =
  "This chat has access to the selected MCP server. Use it to test your tools.";

describe("GramThreadWelcome", () => {
  it("does not claim access while tools are loading", () => {
    mocks.ctx.mcpToolsLoading = true;
    mocks.ctx.mcpTools = undefined;
    mocks.ctx.mcpToolsError = null;

    const html = renderToStaticMarkup(<GramThreadWelcome />);

    expect(html).toContain("Loading tools from this server");
    expect(html).not.toContain(ACCESS_CLAIM);
    expect(html).not.toContain("Explore tools");
  });

  it("replaces the access claim when tools settle empty", () => {
    mocks.ctx.mcpToolsLoading = false;
    mocks.ctx.mcpTools = {};
    mocks.ctx.mcpToolsError = new Error("401 Unauthorized");

    const html = renderToStaticMarkup(<GramThreadWelcome />);

    expect(html).toContain(NO_MCP_TOOLS_MESSAGE);
    expect(html).not.toContain(ACCESS_CLAIM);
    expect(html).not.toContain("Explore tools");
  });

  it("keeps the access claim and suggestions when tools resolve", () => {
    mocks.ctx.mcpToolsLoading = false;
    mocks.ctx.mcpTools = { list_issues: {} };
    mocks.ctx.mcpToolsError = null;

    const html = renderToStaticMarkup(<GramThreadWelcome />);

    expect(html).toContain(ACCESS_CLAIM);
    expect(html).toContain("Explore tools");
    expect(html).not.toContain(NO_MCP_TOOLS_MESSAGE);
  });
});

describe("PlaygroundNoToolsBanner", () => {
  it("stays hidden while tools are loading", () => {
    mocks.ctx.mcpToolsLoading = true;
    mocks.ctx.mcpTools = undefined;
    mocks.ctx.mcpToolsError = null;

    expect(renderToStaticMarkup(<PlaygroundNoToolsBanner />)).toBe("");
  });

  it("renders the honest warning when settled empty", () => {
    mocks.ctx.mcpToolsLoading = false;
    mocks.ctx.mcpTools = {};
    mocks.ctx.mcpToolsError = null;

    const html = renderToStaticMarkup(<PlaygroundNoToolsBanner />);

    expect(html).toContain(NO_MCP_TOOLS_MESSAGE);
  });

  it("stays hidden when tools resolve", () => {
    mocks.ctx.mcpToolsLoading = false;
    mocks.ctx.mcpTools = { list_issues: {} };
    mocks.ctx.mcpToolsError = null;

    expect(renderToStaticMarkup(<PlaygroundNoToolsBanner />)).toBe("");
  });
});
