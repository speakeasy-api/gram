import { describe, expect, it } from "vitest";
import { describeToolActivity } from "./toolActivitySummary";

describe("describeToolActivity", () => {
  it("uses present tense while running and past tense when complete", () => {
    expect(describeToolActivity([{ name: "search_web" }], true)).toBe(
      "Calling Search Web…",
    );
    expect(describeToolActivity([{ name: "search_web" }], false)).toBe(
      "Used Search Web",
    );
  });

  it("counts repeated calls of the same tool", () => {
    expect(
      describeToolActivity(
        [{ name: "search_web" }, { name: "search_web" }],
        false,
      ),
    ).toBe("Used Search Web 2 times");
  });

  it("summarizes a mix of distinct tools by count", () => {
    const calls = [
      { name: "list_deployments" },
      { name: "get_deployment_logs" },
      { name: "search_tools" },
    ];
    expect(describeToolActivity(calls, true)).toBe("Working across 3 tools…");
    expect(describeToolActivity(calls, false)).toBe("Used 3 tools");
  });

  it("describes the request when the turn is only scaffolding tools", () => {
    // "Calling Compose…" tells the user nothing about their own task, so the
    // request becomes the object of the phrase.
    expect(
      describeToolActivity(
        [{ name: "compose" }],
        true,
        "Show me token spend over the last 30 days. Break it down by model.",
      ),
    ).toBe("Investigating token spend over the last 30 days…");
    expect(
      describeToolActivity(
        [{ name: "compose" }],
        false,
        "Scan recent agent conversations for leaked secrets",
      ),
    ).toBe("Investigated recent agent conversations for leaked secrets");
  });

  it("falls back to a neutral phrase when there is no request to describe", () => {
    expect(describeToolActivity([{ name: "compose" }], true)).toBe(
      "Working on it…",
    );
    expect(describeToolActivity([{ name: "compose" }], false)).toBe(
      "Worked on your request",
    );
    // A real tool alongside it still wins.
    expect(
      describeToolActivity([{ name: "compose" }, { name: "search_web" }], true),
    ).toBe("Working across 2 tools…");
  });

  it("degrades gracefully with no tool calls", () => {
    expect(describeToolActivity([], true)).toBe("Working…");
    expect(describeToolActivity([], false)).toBe("Done");
  });
});
