import { beforeEach, describe, expect, it, vi } from "vitest";

// Capture the options passed to createOpenRouter so we can assert on the
// auth headers / baseURL the date picker sends to /chat/completions.
const createOpenRouterMock = vi.fn();

vi.mock("@openrouter/ai-sdk-provider", () => ({
  createOpenRouter: (opts: { headers?: Record<string, string> }) => {
    createOpenRouterMock(opts);
    return { chat: () => ({}) };
  },
}));

vi.mock("ai", () => ({
  generateText: vi.fn(async () => ({
    output: {
      from: "2026-01-01T00:00:00",
      to: "2026-01-01T23:59:59",
      label: "Jan 1",
    },
  })),
  Output: {
    object: vi.fn((opts: unknown) => opts),
  },
}));

// Avoid pulling Datadog RUM (and its window access) into the Node test env.
vi.mock("@/elements/lib/errorTracking", () => ({ trackError: vi.fn() }));

import {
  getPresetRange,
  parseWithAI,
  resolveParsedRange,
} from "./time-range-picker";

describe("parseWithAI request auth", () => {
  beforeEach(() => {
    createOpenRouterMock.mockClear();
  });

  // Root-cause regression test: the /chat/completions proxy authenticates from
  // request headers, so the session credential MUST be forwarded. Before the
  // fix, parseWithAI sent only Gram-Project and relied on a cookie, so this
  // header was absent and the request 401'd (silently).
  it("forwards the session auth header and baseURL to the OpenRouter client", async () => {
    await parseWithAI("yesterday", "https://app.getgram.ai", "proj-slug", {
      "Gram-Session": "test-token",
    });

    expect(createOpenRouterMock).toHaveBeenCalledTimes(1);
    const opts = createOpenRouterMock.mock.calls[0]![0];
    expect(opts.baseURL).toBe("https://app.getgram.ai");
    expect(opts.headers["Gram-Session"]).toBe("test-token");
    expect(opts.headers["Gram-Project"]).toBe("proj-slug");
  });

  it("still sets Gram-Project when no auth headers are provided", async () => {
    await parseWithAI("yesterday", "https://app.getgram.ai", "proj-slug");

    const opts = createOpenRouterMock.mock.calls[0]![0];
    expect(opts.headers["Gram-Project"]).toBe("proj-slug");
    expect(opts.headers["Gram-Session"]).toBeUndefined();
  });
});

describe("resolveParsedRange", () => {
  beforeEach(() => {
    // Freeze time so two getPresetRange calls produce identical ranges.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-13T12:00:00"));
    return () => vi.useRealTimers();
  });

  it("returns a preset unchanged when the preset is offered", () => {
    const parsed = { type: "preset", preset: "7d" } as const;
    expect(resolveParsedRange(parsed, () => true)).toBe(parsed);
  });

  it("passes custom ranges through untouched", () => {
    const parsed = {
      type: "custom",
      range: { from: new Date("2026-01-05"), to: new Date("2026-01-10") },
      label: "1/5-1/10",
    } as const;
    expect(resolveParsedRange(parsed, () => false)).toBe(parsed);
  });

  // Regression test: the billing page passes availablePresets={[]} to hide
  // the quick-pick options, but the AI parser normalizes common phrases
  // ("last week") to presets. Before the fix, such input was silently
  // discarded; it must instead apply as the preset's concrete range.
  it("falls back to the preset's concrete range when the preset is not offered", () => {
    const resolved = resolveParsedRange(
      { type: "preset", preset: "7d" },
      () => false,
    );
    expect(resolved).toEqual({
      type: "custom",
      range: getPresetRange("7d"),
      label: "1w",
    });
  });
});
