import {
  inferenceCapBillingNote,
  inferenceCapInvoiceNote,
} from "@/components/billing/inference-caps";
import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { InferenceCapMeter } from "./inference-cap-meter";

afterEach(cleanup);

function cap(overrides: Partial<InferenceSpendCap> = {}): InferenceSpendCap {
  return {
    keyType: "chat",
    creditsUsed: 10,
    monthlyCredits: 100,
    disabled: false,
    ...overrides,
  };
}

/** The one line under the meter, or null when the meter draws none. */
function footnote(): string | null {
  return screen.queryByText(/this month's cap/i)?.textContent ?? null;
}

describe("InferenceCapMeter", () => {
  // A key with no cap still spends money, and for one of the two key types that
  // money lands on the invoice while for the other it never does. Dropping the
  // note here left the uncapped meter as the one place a customer couldn't tell
  // the two apart.
  describe("without a monthly cap", () => {
    it("still renders the invoiced key's billing note", () => {
      render(
        <InferenceCapMeter
          cap={cap({ keyType: "chat", monthlyCredits: 0 })}
          billingNote
        />,
      );

      expect(
        screen.getByText(/billed to this organization as its own line/i),
      ).toBeTruthy();
    });

    it("still renders the platform-funded key's billing note", () => {
      render(
        <InferenceCapMeter
          cap={cap({ keyType: "internal", monthlyCredits: 0 })}
          billingNote
        />,
      );

      expect(
        screen.getByText(
          /Gram funds this inference, so it never reaches your invoice/,
        ),
      ).toBeTruthy();
    });

    // The note's other half is about the cap's month rolling over, which says
    // there is a cap directly under the line saying there isn't one.
    it.each<["chat" | "internal"]>([["chat"], ["internal"]])(
      "drops the cap-reset copy from the %s key's note",
      (keyType) => {
        render(<InferenceCapMeter cap={cap({ keyType, monthlyCredits: 0 })} />);

        expect(screen.getByText(/No cap is set\./)).toBeTruthy();
        expect(
          screen.queryByText(/resets on the first of the month/),
        ).toBeNull();
      },
    );

    // The uncapped footnote is the invoice sentence and nothing else — not a
    // substring match, so cap-reset copy can't creep back in beside it.
    it.each<["chat" | "internal"]>([["chat"], ["internal"]])(
      "renders exactly the %s key's invoice sentence",
      (keyType) => {
        const { container } = render(
          <InferenceCapMeter
            cap={cap({ keyType, monthlyCredits: 0 })}
            billingNote
          />,
        );

        const paragraphs = container.querySelectorAll("p");
        expect(paragraphs[paragraphs.length - 1]?.textContent).toBe(
          inferenceCapInvoiceNote(keyType),
        );
      },
    );

    it("reports the spend and draws no bar", () => {
      render(
        <InferenceCapMeter
          cap={cap({ creditsUsed: 10, monthlyCredits: 0 })}
          billingNote
        />,
      );

      expect(screen.getByText(/\$10\.00 spent this month/)).toBeTruthy();
      expect(screen.queryByRole("progressbar")).toBeNull();
    });

    // The cap's own control renders the meter without a note, and an empty
    // paragraph there would open a gap under the figure.
    it("renders no footnote when the caller asks for none", () => {
      const { container } = render(
        <InferenceCapMeter cap={cap({ monthlyCredits: 0 })} />,
      );

      expect(container.querySelectorAll("p")).toHaveLength(2);
    });
  });

  // The bands are entered *at* their percentage — `crossedSpendCapThreshold`
  // returns 50 for exactly half — so the boundary is where exclusive wording
  // ("over half") contradicts the figure printed directly above it.
  describe("threshold copy at the band boundaries", () => {
    it.each<[number, RegExp]>([
      [50, /You've used at least half of this month's cap\./],
      [74.99, /You've used at least half of this month's cap\./],
      [75, /You've used at least 75% of this month's cap\./],
      [89.99, /You've used at least 75% of this month's cap\./],
      [90, /You've used at least 90% of this month's cap\./],
      [99.99, /You've used at least 90% of this month's cap\./],
    ])("notes $%s of a $100 cap inclusively", (used, note) => {
      render(<InferenceCapMeter cap={cap({ creditsUsed: used })} />);

      expect(screen.getByText(note)).toBeTruthy();
    });

    it("says nothing below the first band", () => {
      render(<InferenceCapMeter cap={cap({ creditsUsed: 49.99 })} />);

      expect(footnote()).toBeNull();
    });

    it("says the cap is reached at exactly the cap", () => {
      render(<InferenceCapMeter cap={cap({ creditsUsed: 100 })} />);

      expect(footnote()).toMatch(/This month's cap is reached/);
    });

    // The threshold note and the billing note share one line; the threshold
    // comes first because it is the part that has changed.
    it("joins the threshold note to the billing note", () => {
      render(<InferenceCapMeter cap={cap({ creditsUsed: 50 })} billingNote />);

      expect(footnote()).toBe(
        `You've used at least half of this month's cap. ${inferenceCapBillingNote("chat")}`,
      );
    });

    // The cap-reset sentence is dropped only where it would contradict the
    // meter; a cap that is set does reset, and the copy still has to say so.
    it("keeps the cap-reset copy once a cap is set", () => {
      render(<InferenceCapMeter cap={cap({ creditsUsed: 10 })} billingNote />);

      expect(screen.getByText(/resets on the first of the month/)).toBeTruthy();
    });
  });
});
