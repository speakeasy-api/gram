import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import { describe, expect, it } from "vitest";

import {
  crossedSpendCapThreshold,
  inferenceCapFieldId,
  inferenceCapLabel,
  sortInferenceCaps,
  spendCapFillPercent,
} from "./inference-caps";

function cap(overrides: Partial<InferenceSpendCap> = {}): InferenceSpendCap {
  return {
    keyType: "chat",
    creditsUsed: 0,
    monthlyCredits: 100,
    disabled: false,
    ...overrides,
  };
}

describe("inferenceCapLabel", () => {
  // The product names these caps by what they fund. The API's own key types are
  // not those names and must never be what a customer reads.
  it.each<[InferenceSpendCap["keyType"], string]>([
    ["internal", "Security inference cap"],
    ["chat", "Other inference cap"],
  ])("labels the %s key as %s", (keyType, label) => {
    expect(inferenceCapLabel(keyType)).toBe(label);
  });

  const FORBIDDEN = ["chat", "internal", "fallback key"] as const;

  // The correction this section exists for: the misleading identifiers are gone
  // from everything a customer reads.
  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "keeps internal identifiers out of the %s cap's label",
    (keyType) => {
      const label = inferenceCapLabel(keyType).toLowerCase();

      for (const term of FORBIDDEN) {
        expect(label).not.toContain(term);
      }
    },
  );
});

describe("inferenceCapFieldId", () => {
  // Both controls can be on screen together, and a label points at its field by
  // id — a shared one would send every label to the same input.
  it("gives each cap its own field id", () => {
    expect(inferenceCapFieldId("internal")).not.toBe(
      inferenceCapFieldId("chat"),
    );
  });

  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "keeps the %s identifier out of the markup",
    (keyType) => {
      expect(inferenceCapFieldId(keyType)).not.toContain(keyType);
    },
  );
});

describe("crossedSpendCapThreshold", () => {
  // A cap of zero is "none configured", not "spend nothing" — the endpoint
  // reports it for keys that never had one set, and a band above 0 there would
  // read as a limit that was reached.
  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    "reports no band against a cap of %s",
    (limit) => {
      expect(crossedSpendCapThreshold(42, limit)).toBe(0);
    },
  );

  it("reports the highest band the spend has crossed", () => {
    expect(crossedSpendCapThreshold(49.99, 100)).toBe(0);
    expect(crossedSpendCapThreshold(50, 100)).toBe(50);
    expect(crossedSpendCapThreshold(90, 100)).toBe(90);
    expect(crossedSpendCapThreshold(100, 100)).toBe(100);
    expect(crossedSpendCapThreshold(250, 100)).toBe(100);
  });
});

describe("spendCapFillPercent", () => {
  // Spend can pass the cap while the last requests settle, and a bar wider than
  // its track would spill out of the meter.
  it("clamps overage to a full bar", () => {
    expect(spendCapFillPercent(250, 100)).toBe(100);
  });

  it("draws nothing without a cap to be a proportion of", () => {
    expect(spendCapFillPercent(42, 0)).toBe(0);
  });
});

describe("sortInferenceCaps", () => {
  // A refetch that returns the rows in a different order must not reorder the
  // controls under an admin who is typing into one of them.
  it("puts the invoiced cap first whichever order the list arrives in", () => {
    const internal = cap({ keyType: "internal" });
    const chat = cap({ keyType: "chat" });

    expect(sortInferenceCaps([internal, chat]).map((c) => c.keyType)).toEqual([
      "chat",
      "internal",
    ]);
    expect(sortInferenceCaps([chat, internal]).map((c) => c.keyType)).toEqual([
      "chat",
      "internal",
    ]);
  });

  it("handles an organization with one cap, or none", () => {
    expect(sortInferenceCaps([]).length).toBe(0);
    expect(sortInferenceCaps([cap({ keyType: "internal" })]).length).toBe(1);
  });

  it("leaves the query's own array alone", () => {
    const list = [cap({ keyType: "internal" }), cap({ keyType: "chat" })];

    sortInferenceCaps(list);

    expect(list.map((c) => c.keyType)).toEqual(["internal", "chat"]);
  });
});
