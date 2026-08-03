import { describe, expect, it } from "vitest";
import {
  isToolGroupAnnotation,
  normalizeHeading,
  toolChainEnd,
  toolChainStart,
  toolGroupAnnotation,
} from "./toolGroupAnnotation";

const text = (value: string) => ({ type: "text", text: value });
const toolCall = { type: "tool-call" };

describe("toolGroupAnnotation", () => {
  it("uses the text part immediately before the group", () => {
    const parts = [text("Pulling the last 30 days of usage"), toolCall];
    expect(toolGroupAnnotation(parts, 1)).toBe(
      "Pulling the last 30 days of usage",
    );
  });

  it("drops a trailing full stop so the label reads as a heading", () => {
    const parts = [text("Breaking spend down by model."), toolCall];
    expect(toolGroupAnnotation(parts, 1)).toBe("Breaking spend down by model");
  });

  it("ignores a group that opens the message", () => {
    expect(toolGroupAnnotation([toolCall], 0)).toBe("");
  });

  it("ignores a group that follows another tool call", () => {
    expect(toolGroupAnnotation([toolCall, toolCall], 1)).toBe("");
  });

  it("ignores an empty text part", () => {
    expect(toolGroupAnnotation([text("   "), toolCall], 1)).toBe("");
  });
});

describe("normalizeHeading", () => {
  // Every wrong-column example is verbatim from this dashboard's chats. The
  // prompt asks for the doing-phrase form; the model keeps announcing anyway,
  // so the display enforces it deterministically.

  it("keeps a compliant doing-phrase untouched", () => {
    expect(normalizeHeading("Investigating failing tool calls")).toBe(
      "Investigating failing tool calls",
    );
  });

  it("takes the trailing doing-phrase when the model announces first", () => {
    expect(
      normalizeHeading(
        "I'll pull the usage data across those dimensions. Breaking down token spend by tool, model, and client",
      ),
    ).toBe("Breaking down token spend by tool, model, and client");
  });

  it("conjugates a bare announcement into a doing-phrase", () => {
    expect(
      normalizeHeading("I'll pull the risk findings across the project"),
    ).toBe("Pulling the risk findings across the project");
    expect(normalizeHeading("Let me redo it properly")).toBe(
      "Redoing it properly",
    );
    expect(
      normalizeHeading(
        "Now let me get per-model cost breakdowns from the logs.",
      ),
    ).toBe("Getting per-model cost breakdowns from the logs");
  });

  it("prefers the last announcement over an earlier finding", () => {
    expect(
      normalizeHeading(
        "The overview shows zero failures, but it may only count a narrow definition. Let me query logs directly for non-2xx statuses",
      ),
    ).toBe("Querying logs directly for non-2xx statuses");
    expect(
      normalizeHeading("Odd — the loop broke. Let me redo it properly"),
    ).toBe("Redoing it properly");
  });

  it("does not mistake -ing openers like Interesting for a doing-phrase", () => {
    expect(
      normalizeHeading(
        "Interesting — all activity sits in the last two days. Let me check the logs",
      ),
    ).toBe("Checking the logs");
  });

  it("passes text through when no announcement or doing-phrase exists", () => {
    expect(normalizeHeading("No policies and no findings exist yet.")).toBe(
      "No policies and no findings exist yet",
    );
  });
});

describe("tool chains", () => {
  // parts: [text][tool][tool] [text][tool] [text][tool]  [text (answer)]
  // One turn iterating: three runs separated only by narration; the trailing
  // text has no tool call after it, so it stays prose.
  const chain = [
    text("Pulling the usage data across those dimensions"),
    toolCall,
    toolCall,
    text("Checking filter format"),
    toolCall,
    text("Paging without filters instead"),
    toolCall,
    text("Here is the breakdown you asked for."),
  ];

  it("walks a narration-linked run of groups back to its head", () => {
    expect(toolChainStart(chain, 1)).toBe(1);
    expect(toolChainStart(chain, 4)).toBe(1);
    expect(toolChainStart(chain, 6)).toBe(1);
  });

  it("walks forward to the chain's last call", () => {
    expect(toolChainEnd(chain, 2)).toBe(6);
    expect(toolChainEnd(chain, 4)).toBe(6);
    expect(toolChainEnd(chain, 6)).toBe(6);
  });

  it("does not chain across a normal prose gap", () => {
    // A text part with no tool call after it ends the chain; a later,
    // separate group starts a new one.
    const parts = [
      text("Pulling usage"),
      toolCall,
      text("Standalone paragraph with no calls after it does not link"),
      text("Checking the logs"),
      toolCall,
    ];
    expect(toolChainEnd(parts, 1)).toBe(1);
    expect(toolChainStart(parts, 4)).toBe(4);
  });
});

describe("isToolGroupAnnotation", () => {
  it("marks the text a group is showing as its label", () => {
    const parts = [text("Pulling usage"), toolCall];
    // Whatever the group renders must be hidden from the prose, and only that.
    expect(isToolGroupAnnotation(parts, 0)).toBe(true);
    expect(isToolGroupAnnotation(parts, 1)).toBe(false);
  });

  it("leaves text that no group is using", () => {
    expect(isToolGroupAnnotation([text("Here's what I found")], 0)).toBe(false);
    expect(isToolGroupAnnotation([text("   "), toolCall], 0)).toBe(false);
  });
});
