import { describe, expect, it } from "vitest";

import { badgeTone } from "./badgeTone";

describe("badgeTone", () => {
  // A literal list, not Object.keys: dropping a tone must fail the test rather
  // than shrink what it checks.
  it.each(["neutral", "success", "warning", "destructive"] as const)(
    "%s is a non-empty class string",
    (tone) => {
      expect(badgeTone[tone].trim()).not.toBe("");
    },
  );
});
