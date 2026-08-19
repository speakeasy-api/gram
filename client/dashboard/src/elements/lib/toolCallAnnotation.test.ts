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

  it("releases prose as soon as a settled opener rules out a verb", () => {
    // Chat replies open this way constantly; withholding them would delay
    // the first paint of every prose answer.
    for (const text of ["Ok great", "Here is", "I found", "Let me"]) {
      expect(isPartialToolCallAnnotation(text)).toBe(false);
    }
  });

  it("holds a lone opener, which may still be growing into a gerund", () => {
    // "I" is prose on its own and also the first character of
    // "Investigating"; only a following word settles which.
    expect(isPartialToolCallAnnotation("I")).toBe(true);
    expect(isPartialToolCallAnnotation("Sure")).toBe(true);
  });

  it("still holds a two-word opener that could become a doing-phrase", () => {
    // "Deep diving into spend" — word two has not arrived in full yet.
    expect(isPartialToolCallAnnotation("Deep")).toBe(true);
    expect(isPartialToolCallAnnotation("Deep divin")).toBe(true);
  });

  it("releases prose once a third word rules out a doing-phrase", () => {
    expect(isPartialToolCallAnnotation("Cross reference the logs")).toBe(false);
    // Under three words the opener is still undecided.
    expect(isPartialToolCallAnnotation("Cross reference")).toBe(true);
  });

  it("matches a completed gerund ahead of the opener list", () => {
    expect(isPartialToolCallAnnotation("Letting the job finish")).toBe(true);
    expect(isPartialToolCallAnnotation("Noting the gap")).toBe(true);
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
  it("matches only a settled annotation on the last line", () => {
    expect(
      trailingAnnotationLine("Here is what I found\nInvestigating spend"),
    ).toBe("Investigating spend");
    expect(trailingAnnotationLine("Here is what I found\nInvestig")).toBe(
      undefined,
    );
  });
});
