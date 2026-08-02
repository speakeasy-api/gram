import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ToolActivitySummaryInput } from "@/elements/types";

const summarizeMock =
  vi.fn<(input: ToolActivitySummaryInput) => Promise<string | null>>();

vi.mock("@/elements/hooks/useElements", () => ({
  useElements: () => ({
    config: { tools: { summarizeToolActivity: summarizeMock } },
  }),
}));

import { useToolActivitySummary } from "./useToolActivitySummary";

/** Advance past the summarize debounce and flush the resolved summary. */
async function flushSummary() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
}

describe("useToolActivitySummary", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    summarizeMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the heuristic immediately, then swaps in the LLM summary", async () => {
    summarizeMock.mockResolvedValue("Searching the web for pricing");

    const { result } = renderHook(() =>
      useToolActivitySummary({
        toolCalls: [{ name: "search_web" }],
        inProgress: true,
        userMessage: "find pricing",
      }),
    );

    // Instant heuristic before the model responds — shimmering while it settles.
    expect(result.current.label).toBe("Calling Search Web…");
    expect(result.current.enriched).toBe(false);
    expect(result.current.pending).toBe(true);

    await flushSummary();

    expect(result.current.label).toBe("Searching the web for pricing");
    expect(result.current.enriched).toBe(true);
    expect(result.current.pending).toBe(false);
  });

  it("stops shimmering (pending) even when the summary fails", async () => {
    summarizeMock.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() =>
      useToolActivitySummary({
        toolCalls: [{ name: "search_web" }],
        inProgress: true,
      }),
    );

    expect(result.current.pending).toBe(true);

    await flushSummary();

    // Failure resolves pending so the header doesn't shimmer forever.
    expect(result.current.pending).toBe(false);
    expect(result.current.label).toBe("Calling Search Web…");
  });

  it("updates the summary when new tool calls materially change the activity", async () => {
    summarizeMock.mockImplementation(async ({ toolCalls }) => {
      return toolCalls.some((call) => call.name === "edit_file")
        ? "Editing configuration files"
        : "Searching the web for pricing";
    });

    const { result, rerender } = renderHook(
      (props) => useToolActivitySummary(props),
      {
        initialProps: {
          toolCalls: [{ name: "search_web" }],
          inProgress: true,
          userMessage: "fix the config",
        } as Parameters<typeof useToolActivitySummary>[0],
      },
    );

    await flushSummary();
    expect(result.current.label).toBe("Searching the web for pricing");

    // The agent pivots to editing — a materially different set of tools, no
    // interleaved text. The stale "Searching…" label must not linger.
    rerender({
      toolCalls: [{ name: "search_web" }, { name: "edit_file" }],
      inProgress: true,
      userMessage: "fix the config",
    });
    expect(result.current.label).not.toBe("Searching the web for pricing");

    await flushSummary();
    expect(result.current.label).toBe("Editing configuration files");
  });

  it("retains the label across pure growth of the same tool set", async () => {
    summarizeMock.mockResolvedValue("Searching the web for pricing");

    const { result, rerender } = renderHook(
      (props) => useToolActivitySummary(props),
      {
        initialProps: {
          toolCalls: [{ name: "search_web" }],
          inProgress: true,
        } as Parameters<typeof useToolActivitySummary>[0],
      },
    );

    await flushSummary();
    expect(result.current.label).toBe("Searching the web for pricing");

    // Another call to the same tool: same nature, so the label is retained (no
    // flicker back to the heuristic) even before the next summary resolves.
    rerender({
      toolCalls: [{ name: "search_web" }, { name: "search_web" }],
      inProgress: true,
    });
    expect(result.current.label).toBe("Searching the web for pricing");
  });

  it("does not reuse the present-tense label after the tools complete", async () => {
    // Present tense while running; the completion (past-tense) summary fails.
    summarizeMock.mockImplementation(async ({ inProgress }) =>
      inProgress ? "Searching the web for pricing" : null,
    );

    const { result, rerender } = renderHook(
      (props) => useToolActivitySummary(props),
      {
        initialProps: {
          toolCalls: [{ name: "search_web" }],
          inProgress: true,
        } as Parameters<typeof useToolActivitySummary>[0],
      },
    );

    await flushSummary();
    expect(result.current.label).toBe("Searching the web for pricing");

    // On completion the present-tense label must not linger; the past-tense
    // heuristic stands in immediately, and stays when the summary fails.
    rerender({ toolCalls: [{ name: "search_web" }], inProgress: false });
    expect(result.current.label).toBe("Used Search Web");

    await flushSummary();
    expect(result.current.label).toBe("Used Search Web");
  });

  it("resolves to the heuristic when the summarizer hangs", async () => {
    // A summarizer that never resolves on its own but honors the abort signal,
    // like fetch does.
    summarizeMock.mockImplementation(
      ({ signal }) =>
        new Promise((_, reject) => {
          signal?.addEventListener("abort", () => reject(new Error("aborted")));
        }),
    );

    const { result } = renderHook(() =>
      useToolActivitySummary({
        toolCalls: [{ name: "search_web" }],
        inProgress: true,
      }),
    );

    expect(result.current.pending).toBe(true);

    // Past the debounce + request timeout: the hung request is aborted and the
    // header settles on the heuristic rather than shimmering forever.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(400 + 15000 + 10);
    });

    expect(result.current.pending).toBe(false);
    expect(result.current.label).toBe("Calling Search Web…");
  });

  it("never summarizes when disabled (custom-rendered tool)", async () => {
    summarizeMock.mockResolvedValue("should not appear");

    const { result } = renderHook(() =>
      useToolActivitySummary({
        toolCalls: [{ name: "search_web" }],
        inProgress: true,
        enabled: false,
      }),
    );

    expect(result.current.label).toBe("Calling Search Web…");
    await flushSummary();
    expect(result.current.label).toBe("Calling Search Web…");
    expect(summarizeMock).not.toHaveBeenCalled();
  });
});
