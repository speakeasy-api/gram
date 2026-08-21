import { describe, expect, it } from "vitest";

import {
  runDrawsEveryWidget,
  toolSearchVerdict,
} from "./tool-widget-rendering";

let nextId = 0;
const search = (args: Record<string, unknown>) => ({
  type: "tool-call",
  toolCallId: `call_${++nextId}`,
  toolName: "tool_search",
  args,
});
const browse = () => search({ query: "", browse: true });
const discovery = () => search({ query: "logs" });
const plainCall = (toolName = "mcp__p-platform_get_platform_context") => ({
  type: "tool-call",
  toolCallId: `call_${++nextId}`,
  toolName,
  args: {},
});
// Breaks a tool run: only tool calls and the terse annotations before them
// stay in one group.
const prose = () => ({ type: "text", text: "Here is what I found." });

describe("runDrawsEveryWidget", () => {
  it("holds only when every call in the run draws", () => {
    const parts = [browse(), plainCall()];
    expect(runDrawsEveryWidget(parts, [0], undefined)).toBe(true);
    expect(runDrawsEveryWidget(parts, [0, 1], undefined)).toBe(false);
    expect(runDrawsEveryWidget([discovery()], [0], undefined)).toBe(false);
    expect(runDrawsEveryWidget([], [], undefined)).toBe(false);
  });

  it("takes a host override at its word", () => {
    const parts = [plainCall("weather")];
    expect(runDrawsEveryWidget(parts, [0], { weather: () => null })).toBe(true);
  });

  it("does not read a tool name off the prototype", () => {
    // A server may serve a tool called `toString`; the registry has no such
    // entry, and reading one off Object.prototype would invent a widget.
    expect(runDrawsEveryWidget([plainCall("toString")], [0], undefined)).toBe(
      false,
    );
    expect(
      runDrawsEveryWidget([plainCall("constructor")], [0], {
        hasOwnProperty: 1,
      }),
    ).toBe(false);
  });
});

describe("toolSearchVerdict", () => {
  // A verdict per `tool_search` call, in order; every other part reads as
  // undefined, since only the card ever asks.
  const verdicts = (parts: Parameters<typeof toolSearchVerdict>[0]) =>
    parts.map((part) =>
      part.toolName === "tool_search" && part.toolCallId
        ? toolSearchVerdict(parts, part.toolCallId, undefined)
        : undefined,
    );

  it("draws a lone browse", () => {
    expect(verdicts([browse()])).toEqual(["draw"]);
  });

  it("falls back for a discovery search", () => {
    expect(verdicts([discovery()])).toEqual(["fallback"]);
  });

  it("draws once when a model repeats the search", () => {
    // Every search carries the same whole-catalog view, so the duplicates
    // render nothing rather than stacking identical cards.
    expect(verdicts([browse(), browse()])).toEqual(["suppress", "draw"]);
  });

  it("falls back for a browse batched with a plain call", () => {
    // The run cannot be hoisted, so the card would sit behind the disclosure.
    expect(verdicts([browse(), plainCall()])).toEqual(["fallback", undefined]);
  });

  it("keeps the card when a later browse is stranded in a collapsed run", () => {
    // The earlier browse is hoisted and the later one cannot draw. Handing the
    // card to the last browse regardless would leave the message with none.
    const parts = [browse(), prose(), browse(), plainCall()];
    expect(verdicts(parts)).toEqual(["draw", undefined, "fallback", undefined]);
  });

  it("still hands the card to the last drawable browse", () => {
    const parts = [browse(), prose(), browse()];
    expect(verdicts(parts)).toEqual(["suppress", undefined, "draw"]);
  });

  it("falls back for a call that is not in the message", () => {
    expect(toolSearchVerdict([browse()], "call_missing", undefined)).toBe(
      "fallback",
    );
  });
});
