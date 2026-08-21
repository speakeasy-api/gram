import type { ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

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

let nextId = 0;
function searchPart(args: Record<string, unknown>) {
  return {
    type: "tool-call",
    toolCallId: `call_${++nextId}`,
    toolName: "tool_search",
    args,
    argsText: JSON.stringify(args),
    result,
    status: { type: "complete" },
  };
}

const plainCallPart = () => ({
  type: "tool-call",
  toolCallId: `call_${++nextId}`,
  toolName: "mcp__p-platform_get_platform_context",
  args: {},
  result: {},
  status: { type: "complete" },
});

/** Renders one part of a message, as the thread would. */
function render(parts: object[], index: number): string {
  mocks.state.message.parts = parts;
  const props = parts[index] as unknown as ComponentProps<
    typeof ToolSearchResult
  >;
  return renderToStaticMarkup(<ToolSearchResult {...props} />);
}

describe("ToolSearchResult", () => {
  it("draws the catalog for a browse its run hoists", () => {
    expect(render([searchPart({ query: "", browse: true })], 0)).toContain(
      CARD_TEXT,
    );
  });

  it("falls back for a browse batched with a plain call", () => {
    // The run cannot be hoisted, so a card here would sit behind the
    // disclosure that exists to hide a turn's mechanics.
    const markup = render(
      [searchPart({ query: "", browse: true }), plainCallPart()],
      0,
    );
    expect(markup).toContain(FALLBACK_TEXT);
    expect(markup).not.toContain(CARD_TEXT);
  });

  it("falls back for a discovery search", () => {
    const markup = render([searchPart({ query: "logs telemetry" })], 0);
    expect(markup).toContain(FALLBACK_TEXT);
    expect(markup).not.toContain(CARD_TEXT);
  });

  it("renders nothing for a repeated browse", () => {
    const parts = [
      searchPart({ query: "", browse: true }),
      searchPart({ query: "", browse: true }),
    ];
    expect(render(parts, 0)).toBe("");
    expect(render(parts, 1)).toContain(CARD_TEXT);
  });
});
