// @vitest-environment node
import { describe, expect, it } from "vitest";
import { joinInstructions, splitInstructions } from "./sections";

describe("splitInstructions", () => {
  it("separates known sections from a leading preamble", () => {
    const parts = splitInstructions(
      "Be brief.\n\n# Personality\nCalm.\n\n# Tasks\nTriage bugs.\n\n# Behavior\n- Use tools.",
    );
    expect(parts).toEqual({
      preamble: "Be brief.",
      Personality: "Calm.",
      Tasks: "Triage bugs.",
      Behavior: "- Use tools.",
    });
  });

  it("keeps unsectioned instructions whole in the preamble", () => {
    expect(splitInstructions("You are a helpful assistant.").preamble).toBe(
      "You are a helpful assistant.",
    );
  });

  it("round-trips through joinInstructions", () => {
    const text =
      "Intro.\n\n# Personality\nCalm.\n\n# Tasks\nTriage.\n\n# Behavior\n- Use tools.";
    expect(joinInstructions(splitInstructions(text))).toBe(text);
  });

  it("drops empty sections when joining", () => {
    expect(
      joinInstructions({
        preamble: "",
        Personality: "Calm.",
        Tasks: "",
        Behavior: "",
      }),
    ).toBe("# Personality\nCalm.");
  });
});
