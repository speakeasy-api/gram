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
  // A key with no cap set has nothing for a bar to be a proportion of, but the
  // spend against it still has to be somewhere.
  it("reports the spend and draws no bar without a monthly cap", () => {
    render(
      <InferenceCapMeter cap={cap({ creditsUsed: 10, monthlyCredits: 0 })} />,
    );

    expect(screen.getByText(/\$10\.00 spent this month/)).toBeTruthy();
    expect(screen.queryByText(/No cap is set\./)).toBeTruthy();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("names the cap on the bar itself", () => {
    render(<InferenceCapMeter cap={cap({ keyType: "internal" })} />);

    expect(screen.getByRole("progressbar").getAttribute("aria-label")).toBe(
      "Security inference cap: $10.00 of the $100.00 monthly cap",
    );
  });

  // Spend can pass the cap while the last requests settle, and a bar wider than
  // its track would spill out of the meter.
  it("holds the bar at full once the spend passes the cap", () => {
    render(<InferenceCapMeter cap={cap({ creditsUsed: 250 })} />);

    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe(
      "100",
    );
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
  });

  // At the cap, the note says what would start this inference again. For a key
  // the platform has turned off, neither of those things would — the cap is not
  // what is holding it, so naming them promises a recovery that won't come.
  describe("the note at the cap on a key that is turned off", () => {
    it("names no way back that a disabled key doesn't have", () => {
      render(
        <InferenceCapMeter cap={cap({ creditsUsed: 100, disabled: true })} />,
      );

      const note = footnote();
      expect(note).toMatch(/This month's cap is reached/);
      expect(note).toMatch(/turned off for this organization/i);
      expect(note).not.toMatch(/stopped until/i);
      expect(note).toMatch(
        /neither the month rolling over nor a higher cap will resume it/i,
      );
    });

    // The same key, still on: the cap is what stopped it, so the way back is
    // the way back.
    it("still names both ways back when the key is on", () => {
      render(
        <InferenceCapMeter cap={cap({ creditsUsed: 100, disabled: false })} />,
      );

      expect(footnote()).toBe(
        "This month's cap is reached, so this inference is stopped until the month rolls over or the cap is raised.",
      );
    });

    // Only the 100 band makes that promise, so only the 100 band changes.
    it.each<[number, RegExp]>([
      [50, /You've used at least half of this month's cap\./],
      [75, /You've used at least 75% of this month's cap\./],
      [90, /You've used at least 90% of this month's cap\./],
    ])("leaves the %s band's note alone", (used, note) => {
      render(
        <InferenceCapMeter cap={cap({ creditsUsed: used, disabled: true })} />,
      );

      expect(screen.getByText(note)).toBeTruthy();
    });
  });
});
