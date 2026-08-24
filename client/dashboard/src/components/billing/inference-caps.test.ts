import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import { describe, expect, it } from "vitest";

import {
  INFERENCE_CAPS_ANCHOR,
  inferenceCapAnchor,
  inferenceCapBillingNote,
  inferenceCapInvoiceNote,
  inferenceCapLabel,
  inferenceCapPausedNote,
  inferenceCapRaiseLabel,
  inferenceSpendHint,
  inferenceSpendLabel,
  isInferenceCapAnchor,
  isInferenceCapReached,
  sortInferenceCaps,
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
    ["chat", "Customer-facing inference cap"],
  ])("labels the %s key as %s", (keyType, label) => {
    expect(inferenceCapLabel(keyType)).toBe(label);
  });
});

describe("inferenceSpendLabel", () => {
  it.each<[InferenceSpendCap["keyType"], string]>([
    ["internal", "Security"],
    ["chat", "Customer-facing"],
  ])("labels the %s key as %s", (keyType, label) => {
    expect(inferenceSpendLabel(keyType)).toBe(label);
  });
});

describe("inferenceSpendHint", () => {
  it("explains customer-facing spend without the leftover Other name", () => {
    expect(inferenceSpendHint("chat")).toMatch(/assistants/i);
    expect(inferenceSpendHint("chat")).not.toMatch(/other inference/i);
  });

  it("explains security spend as automated analysis", () => {
    expect(inferenceSpendHint("internal")).toMatch(/automated analysis/i);
  });
});

describe("inferenceCapAnchor", () => {
  it("gives each cap its own anchor", () => {
    expect(inferenceCapAnchor("internal")).not.toBe(inferenceCapAnchor("chat"));
  });

  // The anchor lands in the address bar, so the API's key types must not be in
  // it any more than they are in the copy.
  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "keeps the %s identifier out of the URL",
    (keyType) => {
      expect(inferenceCapAnchor(keyType)).not.toContain(keyType);
    },
  );

  it("recognizes the section anchor and every cap anchor", () => {
    expect(isInferenceCapAnchor(INFERENCE_CAPS_ANCHOR)).toBe(true);
    expect(isInferenceCapAnchor(inferenceCapAnchor("internal"))).toBe(true);
    expect(isInferenceCapAnchor(inferenceCapAnchor("chat"))).toBe(true);
  });

  it("recognizes nothing else", () => {
    expect(isInferenceCapAnchor("billing-email")).toBe(false);
    expect(isInferenceCapAnchor("")).toBe(false);
  });
});

describe("cap copy", () => {
  // The banners stack when both caps are reached, so their calls to action have
  // to be told apart by name alone.
  it("names each cap's call to action distinctly", () => {
    expect(inferenceCapRaiseLabel("internal")).not.toBe(
      inferenceCapRaiseLabel("chat"),
    );
  });

  // The two caps stop unrelated work; a banner that described the wrong one
  // would send an admin to raise a cap that isn't the reason for the silence.
  it("describes what each cap stops in its own words", () => {
    expect(inferenceCapPausedNote("internal")).not.toBe(
      inferenceCapPausedNote("chat"),
    );
  });

  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "includes the %s key in PAYG billing",
    (keyType) => {
      expect(inferenceCapInvoiceNote(keyType)).toMatch(
        /included in the inference spend.*invoice/i,
      );
      expect(inferenceCapInvoiceNote(keyType)).not.toMatch(/estimate above/i);
    },
  );

  // The uncapped meter renders the invoice half on its own, so it has to read
  // as a whole sentence rather than as a fragment of the note it comes from.
  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "makes the %s cap's invoice sentence stand alone",
    (keyType) => {
      expect(inferenceCapBillingNote(keyType)).toContain(
        inferenceCapInvoiceNote(keyType),
      );
      expect(inferenceCapInvoiceNote(keyType)).not.toContain("resets");
    },
  );

  const FORBIDDEN = ["chat", "internal", "spend cap", "fallback key"] as const;

  // The correction this section exists for: the misleading identifiers are gone
  // from everything a customer reads.
  it.each<InferenceSpendCap["keyType"]>(["internal", "chat"])(
    "keeps internal identifiers out of the %s cap's copy",
    (keyType) => {
      const copy = [
        inferenceCapLabel(keyType),
        inferenceSpendLabel(keyType),
        inferenceSpendHint(keyType),
        inferenceCapPausedNote(keyType),
        inferenceCapRaiseLabel(keyType),
        inferenceCapBillingNote(keyType),
      ]
        .join(" ")
        .toLowerCase();

      for (const term of FORBIDDEN) {
        expect(copy).not.toContain(term);
      }
    },
  );
});

describe("isInferenceCapReached", () => {
  // A cap of zero is "none configured", not "spend nothing" — the endpoint
  // reports it for keys that never had one set.
  const cases: Array<{
    name: string;
    usage: { monthlyCredits: number; creditsUsed: number } | undefined;
    reached: boolean;
  }> = [
    { name: "nothing loaded", usage: undefined, reached: false },
    {
      name: "no cap configured and nothing spent",
      usage: { monthlyCredits: 0, creditsUsed: 0 },
      reached: false,
    },
    {
      name: "no cap configured but spending",
      usage: { monthlyCredits: 0, creditsUsed: 42 },
      reached: false,
    },
    {
      name: "under the cap",
      usage: { monthlyCredits: 100, creditsUsed: 99 },
      reached: false,
    },
    {
      name: "exactly at the cap",
      usage: { monthlyCredits: 100, creditsUsed: 100 },
      reached: true,
    },
    {
      name: "over the cap",
      usage: { monthlyCredits: 100, creditsUsed: 150 },
      reached: true,
    },
  ];

  for (const { name, usage, reached } of cases) {
    it(`${reached ? "pauses" : "runs"} with ${name}`, () => {
      expect(isInferenceCapReached(usage)).toBe(reached);
    });
  }
});

describe("sortInferenceCaps", () => {
  // A refetch that returns the rows in a different order must not reorder the
  // controls under an admin who is typing into one of them.
  it("keeps the product order whichever order the list arrives in", () => {
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
