import { expect, it } from "vitest";

import { categoryLabel, checkFailureLabel } from "./aiProductLabels";

it("labels known check failures and keeps the code", () => {
  expect(checkFailureLabel("contains_secret")).toBe(
    "Document contains a client secret field, which is not allowed (contains_secret)",
  );
});

it("falls back for a code it does not know", () => {
  expect(checkFailureLabel("something_new")).toBe(
    "Check failed (something_new)",
  );
  expect(checkFailureLabel("__proto__")).toBe("Check failed (__proto__)");
});

it("labels categories", () => {
  expect(categoryLabel("harness")).toBe("Coding agent");
  expect(categoryLabel(undefined)).toBe("No category");
  expect(categoryLabel("__proto__")).toBe("__proto__");
  expect(categoryLabel("toString")).toBe("toString");
});
