import { describe, expect, it } from "vitest";

import { badgeTone } from "./badgeTone";
import { tone } from "./tone";

// A literal list, not Object.keys: dropping a tone must fail these tests rather
// than shrink what they check.
const TONES = ["neutral", "success", "warning", "destructive"] as const;

describe("badgeTone", () => {
  // The colours are measured in `tone.test.ts`. What is left to hold here is
  // that a badge still gets them: a tone rewritten with its own copy of the
  // palette would pass every contrast test and still drift.
  it.each(TONES)("%s draws in the shared tone", (name) => {
    expect(badgeTone[name]).toContain(tone[name]);
  });

  it.each(TONES)("%s adds the badge's own casing", (name) => {
    expect(badgeTone[name].split(" ")).toContain("uppercase");
  });
});
