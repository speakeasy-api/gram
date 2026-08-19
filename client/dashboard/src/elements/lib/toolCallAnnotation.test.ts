import { describe, expect, it } from "vitest";
import {
  isPartialToolCallAnnotation,
  isToolCallAnnotation,
  trailingAnnotationLine,
} from "./toolCallAnnotation";

describe("isPartialToolCallAnnotation", () => {
  it("accepts the prefixes an annotation streams through", () => {
    // "Investigating failures" arrives a character at a time; every prefix
    // must be held back, or the opening renders as prose and then retracts.
    const target = "Investigating failures";
    for (let i = 1; i <= target.length; i++) {
      expect(isPartialToolCallAnnotation(target.slice(0, i))).toBe(true);
    }
  });

  it("releases prose once a third word rules out a doing-phrase", () => {
    expect(isPartialToolCallAnnotation("Let me check the logs")).toBe(false);
    // Under three words the opener is still undecided.
    expect(isPartialToolCallAnnotation("Let me")).toBe(true);
  });

  it("still rejects prose by shape", () => {
    expect(isPartialToolCallAnnotation("**Checking**")).toBe(false);
    expect(isPartialToolCallAnnotation("Checking.\nNext")).toBe(false);
    expect(isPartialToolCallAnnotation("x".repeat(101))).toBe(false);
  });

  it("accepts everything the strict test accepts", () => {
    for (const text of ["Investigating failures", "Deep diving into spend"]) {
      expect(isToolCallAnnotation(text)).toBe(true);
      expect(isPartialToolCallAnnotation(text)).toBe(true);
    }
  });
});

describe("trailingAnnotationLine", () => {
  it("uses the strict test by default and the partial one when streaming", () => {
    expect(trailingAnnotationLine("Here is what I found\nInvestig")).toBe(
      undefined,
    );
    expect(
      trailingAnnotationLine("Here is what I found\nInvestig", {
        streaming: true,
      }),
    ).toBe("Investig");
  });
});
