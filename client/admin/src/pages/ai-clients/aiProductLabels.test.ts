import { expect, it } from "vitest";

import { categoryLabel, checkFailureLabel } from "./aiProductLabels";

it("labels known check failures and keeps the code", () => {
  expect(checkFailureLabel("contains_secret")).toBe(
    "Document contains a client secret (contains_secret)",
  );
});

it("falls back for a code it does not know", () => {
  expect(checkFailureLabel("something_new")).toBe(
    "Check failed (something_new)",
  );
});

it("labels categories", () => {
  expect(categoryLabel("harness")).toBe("Coding agent");
  expect(categoryLabel(undefined)).toBe("None (sign-in documents only)");
});
