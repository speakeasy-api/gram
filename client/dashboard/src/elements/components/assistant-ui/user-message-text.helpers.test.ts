import { describe, expect, it } from "vitest";

import { splitContextBlocks } from "./user-message-text.helpers";

describe("splitContextBlocks", () => {
  it("peels a leading dashboard_context block off the human text", () => {
    const text =
      "<dashboard_context>\nCost dashboard — Engineering.\n</dashboard_context>\n\nWhat caused the spike?";
    const { blocks, rest } = splitContextBlocks(text);
    expect(blocks).toEqual([
      { tag: "dashboard_context", body: "Cost dashboard — Engineering." },
    ]);
    expect(rest).toBe("What caused the spike?");
  });

  it("peels multiple consecutive context blocks", () => {
    const text =
      "<message-context>EventID: 1</message-context><dashboard_context>chart=top-users</dashboard_context>hi";
    const { blocks, rest } = splitContextBlocks(text);
    expect(blocks.map((b) => b.tag)).toEqual([
      "message-context",
      "dashboard_context",
    ]);
    expect(rest).toBe("hi");
  });

  it("leaves a plain message untouched", () => {
    const { blocks, rest } = splitContextBlocks("just a question");
    expect(blocks).toEqual([]);
    expect(rest).toBe("just a question");
  });

  it("only folds blocks at the start, not context tags inside the message", () => {
    const text = "explain <foo_context>x</foo_context> please";
    const { blocks, rest } = splitContextBlocks(text);
    expect(blocks).toEqual([]);
    expect(rest).toBe(text);
  });

  it("handles a context-only turn (no human text)", () => {
    const { blocks, rest } = splitContextBlocks(
      "<dashboard_context>only</dashboard_context>",
    );
    expect(blocks).toHaveLength(1);
    expect(rest).toBe("");
  });
});
