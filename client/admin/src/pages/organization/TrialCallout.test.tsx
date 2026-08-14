import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TRIAL_STATES } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";
import { fmtDateShort } from "@/lib/utils";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { TrialCallout } from "./TrialCallout";

// The states with a deadline still ahead of them. Written out rather than
// imported from the module under test: importing the set would move this
// expectation along with the rule it is meant to hold in place.
const SHOWS_CALLOUT = new Set(["running", "ending_soon"]);

// UTC and this zone fall on different dates, so a date read in the reader's
// zone instead of the record's renders a day early and fails below. CI runs in
// UTC, where that fault cannot appear at all.
const TRIAL_ENDS_AT = "2026-05-06T00:00:00Z";

beforeEach(() => {
  vi.stubEnv("TZ", "America/Los_Angeles");
});

afterEach(() => {
  vi.unstubAllEnvs();
  cleanup();
});

describe("TrialCallout", () => {
  // Walking TRIAL_STATES rather than a hand-written list means a seventh state
  // added to the union fails this test instead of silently defaulting.
  it.each(TRIAL_STATES)(
    "renders for %s only while the trial is live",
    async (state) => {
      await renderWithApp(
        <TrialCallout org={anOrganization({ trial_state: state })} />,
      );

      expect(Boolean(screen.queryByRole("status"))).toBe(
        SHOWS_CALLOUT.has(state),
      );
    },
  );

  it("renders nothing when the record has no trial_state at all", async () => {
    await renderWithApp(<TrialCallout org={anOrganization()} />);

    expect(screen.queryByRole("status")).toBeNull();
  });

  it("reads the end date in the record's zone, not the reader's", async () => {
    await renderWithApp(
      <TrialCallout
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: TRIAL_ENDS_AT,
        })}
      />,
    );

    const callout = screen.getByRole("status");
    expect(callout.textContent).toContain(fmtDateShort(TRIAL_ENDS_AT));
  });

  it("names the state when the record carries no end date", async () => {
    await renderWithApp(
      <TrialCallout org={anOrganization({ trial_state: "ending_soon" })} />,
    );

    const callout = screen.getByRole("status");
    expect(callout.textContent).toContain(TRIAL_LABELS.ending_soon);
    // A sentence that trails off into nothing is worse than none: the operator
    // reads it as a deadline the page failed to load.
    expect(callout.textContent).not.toContain("-");
  });

  // Both live states, not just the first. The callout draws for either one, so
  // a state it draws for but offers nothing in is a banner with no way to act
  // on it.
  it.each(["running", "ending_soon"] as const)(
    "hosts the record's actions while the trial is %s",
    async (state) => {
      const org = anOrganization({
        trial_state: state,
        trial_ends_at: TRIAL_ENDS_AT,
      });
      await renderWithApp(<TrialCallout org={org} />);

      const callout = screen.getByRole("status");
      expect(
        callout.contains(
          screen.getByRole("button", { name: `Extend trial for ${org.name}` }),
        ),
      ).toBe(true);
    },
  );

  it("leaves extend off for a disabled organization, whatever its trial says", async () => {
    const org = anOrganization({
      trial_state: "running",
      trial_ends_at: TRIAL_ENDS_AT,
      disabled_at: "2026-03-04T00:00:00Z",
    });
    await renderWithApp(<TrialCallout org={org} />);

    expect(
      screen.queryByRole("button", { name: `Extend trial for ${org.name}` }),
    ).toBeNull();
  });
});
