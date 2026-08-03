import { describe, expect, it } from "vitest";
import {
  isToolGroupAnnotation,
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

  it("ignores prose: multi-paragraph or overlong text is an answer", () => {
    expect(
      toolGroupAnnotation([text("A finding.\n\nNext up"), toolCall], 1),
    ).toBe("");
    expect(toolGroupAnnotation([text("x".repeat(201)), toolCall], 1)).toBe("");
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
    expect(
      isToolGroupAnnotation([text("A finding.\n\nNext up"), toolCall], 0),
    ).toBe(false);
  });
});
