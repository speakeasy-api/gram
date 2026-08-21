import type { ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { CollapsedToolRunProvider } from "@/elements/contexts/CollapsedToolRunContext";

const mocks = vi.hoisted(() => ({
  state: {
    thread: { composer: { text: "" } },
    message: { parts: [] as unknown[] },
  },
}));

vi.mock("@assistant-ui/react", () => ({
  useAui: () => ({
    thread: () => ({ composer: () => ({ setText: vi.fn(), send: vi.fn() }) }),
  }),
  useAuiState: (selector: (state: unknown) => unknown) => selector(mocks.state),
}));

vi.mock("@/elements/hooks/useElements", () => ({
  useElements: () => ({ config: {} }),
}));

// The generic card renders shiki-highlighted JSON and is covered elsewhere;
// standing it in keeps this about which of the two the component chooses.
vi.mock("@/elements/components/assistant-ui/tool-fallback", () => ({
  ToolFallback: () => <div>generic tool card</div>,
}));

import { ToolSearchResult } from "./tool-search-result";

const CARD_TEXT = "available in this session";
const FALLBACK_TEXT = "generic tool card";

const result = {
  servers: [
    { id: "_p-platform", tools: ["mcp__p-platform_get_platform_context"] },
  ],
  catalog: [
    {
      name: "mcp__p-platform_get_platform_context",
      brief: "Reads the project context.",
    },
  ],
};

function render(args: Record<string, unknown>, collapsedRun: boolean): string {
  const props = {
    type: "tool-call",
    toolCallId: "call_1",
    toolName: "tool_search",
    args,
    argsText: JSON.stringify(args),
    result,
    status: { type: "complete" },
  } as unknown as ComponentProps<typeof ToolSearchResult>;
  mocks.state.message.parts = [props];

  return renderToStaticMarkup(
    <CollapsedToolRunProvider value={collapsedRun}>
      <ToolSearchResult {...props} />
    </CollapsedToolRunProvider>,
  );
}

describe("ToolSearchResult", () => {
  it("draws the catalog for a browse the run hoisted", () => {
    expect(render({ query: "", browse: true }, false)).toContain(CARD_TEXT);
  });

  it("falls back inside a collapsed run", () => {
    // The run held a plain tool call too, so the group could not be hoisted.
    // A catalog folded into a disclosure is worse than no catalog: in there
    // this is one more call among the mechanics.
    const markup = render({ query: "", browse: true }, true);
    expect(markup).toContain(FALLBACK_TEXT);
    expect(markup).not.toContain(CARD_TEXT);
  });

  it("falls back for a discovery search either way", () => {
    for (const collapsed of [false, true]) {
      const markup = render({ query: "logs telemetry" }, collapsed);
      expect(markup).toContain(FALLBACK_TEXT);
      expect(markup).not.toContain(CARD_TEXT);
    }
  });
});
