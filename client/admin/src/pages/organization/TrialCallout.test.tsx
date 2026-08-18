import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TRIAL_STATES } from "@/lib/gramAdminApi";
import { tone } from "@/lib/tone";
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

  // Both live states, not just the first: `canExtendTrial` inlined as
  // `trial_state === "running"` passes the first case and fails this one.
  it.each(["running", "ending_soon"] as const)(
    "carries the trial's own action and not the record's while %s",
    async (state) => {
      await renderWithApp(
        <TrialCallout
          org={anOrganization({
            trial_state: state,
            trial_ends_at: TRIAL_ENDS_AT,
          })}
        />,
      );

      // Screen-wide, not scoped to the status region: an action moved to a
      // sibling of that region is still an action on the callout. Task 6b found
      // that a `within(callout)` query cannot see one.
      const labels = screen.queryAllByRole("button").map((b) => b.textContent);
      expect(labels).toContain("Extend trial");
      expect(labels).not.toContain("Disable");
    },
  );

  it("puts the action at the far end of the callout, opposite the deadline", async () => {
    await renderWithApp(
      <TrialCallout
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: TRIAL_ENDS_AT,
        })}
      />,
    );

    // Read off the class, because happy-dom lays nothing out and this is the
    // only account of the rule available here. The button belongs at the
    // opposite end from the date it acts on, and the bar wraps rather than
    // pushing the deadline off a narrow viewport.
    const callout = screen.getByRole("status").className.split(" ");
    expect(callout).toContain("justify-between");
    expect(callout).toContain("flex-wrap");
  });

  it("draws in the warning tone rather than the page's own grey", async () => {
    await renderWithApp(
      <TrialCallout
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: TRIAL_ENDS_AT,
        })}
      />,
    );

    // happy-dom lays nothing out and computes no colour, so the class is the
    // only account of the tone available here. Every class of the tone, not the
    // string: `cn` may drop one against the layout classes beside it.
    const classes = screen.getByRole("status").className.split(" ");
    expect(classes).toEqual(expect.arrayContaining(tone.warning.split(" ")));
    // The state the design's notes say the admin must not miss. In the muted
    // grey it read as one more panel.
    expect(classes).not.toContain("bg-muted/30");
  });

  it("draws its action as a control of the same tone, not the page's own", async () => {
    await renderWithApp(
      <TrialCallout
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: TRIAL_ENDS_AT,
        })}
      />,
    );

    const classes = screen.getByRole("button").className.split(" ");
    // The tone's border, taken from the tone itself rather than written out a
    // second time.
    expect(classes).toEqual(
      expect.arrayContaining(
        tone.warning.split(" ").filter((c) => c.includes("border-[")),
      ),
    );
    // Unfilled, so the callout shows through. A stock outline button brings
    // the page's own background and shadow into the middle of the panel.
    expect(classes).toContain("bg-transparent");
    expect(classes).toContain("shadow-none");
    expect(classes).not.toContain("bg-background");
    // The tone's own background is not a button fill either: `cn` has to drop
    // it, or the control reads as a second panel.
    expect(classes).not.toContain("bg-[hsl(29_100%_95%)]");
  });

  it("offers no extension for a disabled organization, whatever its trial says", async () => {
    // The server would take the request: nothing in the extend handler reads
    // disabled_at. Offering it is offering to buy more of a trial nobody can
    // use, and the callout is now the only place the record offers it at all.
    await renderWithApp(
      <TrialCallout
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: TRIAL_ENDS_AT,
          disabled_at: "2026-03-04T00:00:00Z",
        })}
      />,
    );

    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });
});
