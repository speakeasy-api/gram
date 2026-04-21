import { describe, expect, it } from "vitest";
import type { ModelMessage } from "ai";
import {
  compactBySlidingWindow,
  compactForModel,
  DEFAULT_CONTEXT_LIMIT,
  estimateTokens,
  getModelContextLimit,
} from "./contextCompaction";

function msg(
  role: "system" | "user" | "assistant" | "tool",
  content: string,
): ModelMessage {
  return { role, content } as ModelMessage;
}

describe("estimateTokens", () => {
  it("returns roughly chars/4", () => {
    const messages = [msg("user", "a".repeat(400))];
    const n = estimateTokens(messages);
    // Actual output is JSON-wrapped so it's slightly larger than 100
    expect(n).toBeGreaterThan(100);
    expect(n).toBeLessThan(200);
  });

  it("grows with message count", () => {
    const one = estimateTokens([msg("user", "hello")]);
    const many = estimateTokens(
      Array.from({ length: 100 }, () => msg("user", "hello")),
    );
    expect(many).toBeGreaterThan(one * 50);
  });
});

describe("getModelContextLimit", () => {
  it("returns known mapping for Sonnet 4.6", () => {
    expect(getModelContextLimit("anthropic/claude-sonnet-4.6")).toBe(1_000_000);
  });

  it("returns known mapping for Claude 4 (non-1M)", () => {
    expect(getModelContextLimit("anthropic/claude-sonnet-4")).toBe(200_000);
  });

  it("returns DEFAULT_CONTEXT_LIMIT for unknown models", () => {
    expect(getModelContextLimit("acme/very-new-model")).toBe(
      DEFAULT_CONTEXT_LIMIT,
    );
  });
});

describe("compactBySlidingWindow", () => {
  it("no-ops when under the limit", () => {
    const messages = [msg("user", "hi"), msg("assistant", "hello")];
    const result = compactBySlidingWindow(messages, 1_000_000);
    expect(result.droppedCount).toBe(0);
    expect(result.messages).toBe(messages);
  });

  it("drops oldest non-system turns to fit", () => {
    // 10 bulky messages, tiny limit → forces dropping
    const messages: ModelMessage[] = [];
    for (let i = 0; i < 10; i++) {
      messages.push(msg("user", `query-${i} ` + "x".repeat(400)));
      messages.push(msg("assistant", `reply-${i} ` + "y".repeat(400)));
    }
    const maxTokens = 500;
    const result = compactBySlidingWindow(messages, maxTokens, 4);
    expect(result.droppedCount).toBeGreaterThan(0);
    expect(result.estimatedTokensAfter).toBeLessThanOrEqual(
      result.estimatedTokensBefore,
    );
    // Last 4 are preserved verbatim
    const tail = result.messages.slice(-4);
    expect(tail[tail.length - 1]).toEqual(messages[messages.length - 1]);
    // Marker prepended
    const markerPresent = result.messages.some(
      (m) => typeof m.content === "string" && m.content.includes("omitted"),
    );
    expect(markerPresent).toBe(true);
  });

  it("always preserves system messages", () => {
    const messages: ModelMessage[] = [
      msg("system", "sys " + "s".repeat(1000)),
      ...Array.from({ length: 20 }, (_, i) =>
        msg("user", `q-${i} ` + "x".repeat(500)),
      ),
    ];
    const result = compactBySlidingWindow(messages, 300, 2);
    expect(result.droppedCount).toBeGreaterThan(0);
    expect(result.messages[0]!.role).toBe("system");
  });

  it("preserves at least keepRecent messages even if over limit", () => {
    const messages = Array.from({ length: 10 }, (_, i) =>
      msg("user", "x".repeat(1000) + `-${i}`),
    );
    const result = compactBySlidingWindow(messages, 10, 3);
    // keepRecent preserved even though we can't get under the limit
    expect(result.messages.length).toBeGreaterThanOrEqual(3);
    // Last 3 are intact
    const tail = result.messages.slice(-3);
    expect(tail).toEqual(messages.slice(-3));
  });
});

describe("compactBySlidingWindow — tool message pairing", () => {
  it("never leaves a tool message at the head of the retained window", () => {
    // Scenario from Devin: dropping oldest-first could split an
    // assistant(tool_calls) → tool pair, leaving an orphan tool at the
    // head of the retained set. Providers reject this with a 400.
    const messages: ModelMessage[] = [
      msg("user", "q1 " + "x".repeat(400)),
      msg("assistant", "a1-with-tool-call " + "x".repeat(400)),
      msg("tool", "t1-result " + "x".repeat(400)),
      msg("assistant", "a1-final " + "x".repeat(400)),
      msg("user", "q2 " + "x".repeat(400)),
      msg("assistant", "a2-with-tool-call " + "x".repeat(400)),
      msg("tool", "t2-result " + "x".repeat(400)),
      msg("assistant", "a2-final " + "x".repeat(400)),
    ];

    const result = compactBySlidingWindow(messages, 400, 4);
    expect(result.droppedCount).toBeGreaterThan(0);

    // The retained non-system messages should never start with a tool.
    const nonSystem = result.messages.filter((m) => m.role !== "system");
    // Skip the synthetic assistant marker if present.
    const firstReal = nonSystem.find(
      (m) =>
        !(
          m.role === "assistant" &&
          typeof m.content === "string" &&
          m.content.includes("omitted")
        ),
    );
    expect(firstReal?.role).not.toBe("tool");
  });

  it("drops an assistant+tool pair atomically (not one without the other)", () => {
    const messages: ModelMessage[] = [
      msg("user", "old"),
      msg("assistant", "calling tool"),
      msg("tool", "result " + "x".repeat(2000)),
      msg("user", "recent " + "x".repeat(200)),
      msg("assistant", "recent reply " + "x".repeat(200)),
    ];
    const result = compactBySlidingWindow(messages, 300, 2);
    // If the group was dropped atomically, both the assistant and its tool
    // are gone together. If the bug was still present, we'd see the tool
    // message lingering alone.
    const nonSystem = result.messages.filter((m) => m.role !== "system");
    const hasLoneTool = nonSystem.some(
      (m, i) =>
        m.role === "tool" && (i === 0 || nonSystem[i - 1]!.role === "user"),
    );
    expect(hasLoneTool).toBe(false);
  });

  it("does not split a tool group when aligning the recent window", () => {
    // keepRecent=3 would cut mid-group with naive slicing. Grouping should
    // expand the recent window to keep the assistant+tools together.
    const messages: ModelMessage[] = [
      msg("user", "old " + "x".repeat(1000)),
      msg("assistant", "calling 2 tools"),
      msg("tool", "result1"),
      msg("tool", "result2"),
      msg("assistant", "final"),
    ];
    const result = compactBySlidingWindow(messages, 200, 3);
    // If the first tool was the "recent" cut-off, we'd see a tool at
    // the head of retained — but grouping should have pulled the
    // assistant with it.
    const kept = result.messages.filter((m) => m.role !== "system");
    const firstTool = kept.findIndex((m) => m.role === "tool");
    if (firstTool !== -1) {
      expect(kept[firstTool - 1]?.role).toMatch(/assistant|tool/);
    }
  });
});

describe("compactForModel", () => {
  it("uses 70% of the nominal ceiling by default", () => {
    const small = [msg("user", "hi")];
    const result = compactForModel(small, "anthropic/claude-sonnet-4.6");
    expect(result.droppedCount).toBe(0);
    expect(result.messages).toBe(small);
  });

  it("honors explicit maxTokens override", () => {
    const messages = Array.from({ length: 30 }, (_, i) =>
      msg("user", "x".repeat(500) + `-${i}`),
    );
    const result = compactForModel(messages, "anthropic/claude-sonnet-4.6", {
      maxTokens: 2000,
      keepRecent: 2,
    });
    expect(result.droppedCount).toBeGreaterThan(0);
  });
});
