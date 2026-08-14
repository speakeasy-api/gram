import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Trial } from "@/components/Trial";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization, TrialState } from "@/lib/gramAdminApi";

const TRIAL_ENDS_AT = "2026-05-06T00:00:00Z";

// The stale pair is set, and set to a different date, on every record here.
// A surface that goes back to reading `free_trial_ends_at` then renders the
// wrong date rather than the right one by luck.
const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "pro",
  whitelisted: true,
  free_trial_started_at: "2026-02-01T00:00:00Z",
  free_trial_ends_at: "2026-11-12T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

function orgWith(trial: Partial<AdminOrganization>): AdminOrganization {
  return { ...ORG, ...trial };
}

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}

function renderTrial(org: AdminOrganization): void {
  render(
    <div data-testid="trial">
      <Trial org={org} />
    </div>,
  );
}

function trialText(): string {
  return screen.getByTestId("trial").textContent ?? "";
}

function queryBadge(): HTMLElement | null {
  return screen
    .getByTestId("trial")
    .querySelector<HTMLElement>('[data-slot="badge"]');
}

function badge(): HTMLElement {
  const found = queryBadge();
  if (!found) throw new Error("the state is not on a badge");
  return found;
}

afterEach(cleanup);

describe("Trial", () => {
  it("reads a dash for an organization that never trialled", () => {
    renderTrial(orgWith({ trial_state: "none", trial_ends_at: undefined }));

    // The stale field carries a date on this record. A dash is the only
    // reading that proves the cell is not on the old field.
    expect(trialText()).toBe("-");
    expect(queryBadge()).toBeNull();
  });

  it("reads a dash when the API sends no trial state at all", () => {
    renderTrial(ORG);

    expect(trialText()).toBe("-");
    expect(queryBadge()).toBeNull();
  });

  it("falls back to the dash for a state this build does not know", () => {
    renderTrial(
      orgWith({
        // A seventh state the server may derive before this build ships.
        trial_state: "paused" as TrialState,
        trial_ends_at: TRIAL_ENDS_AT,
      }),
    );

    // Not the raw enum name, and not a crash.
    expect(trialText()).toBe("-");
    expect(queryBadge()).toBeNull();
  });

  it("puts the end date beside a running trial and tones it as normal", () => {
    renderTrial(
      orgWith({ trial_state: "running", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe(`Running${shortDate(TRIAL_ENDS_AT)}`);
    // Neutral, not warning: the server has a separate state for a trial about
    // to end, and toning every running trial as urgent wastes it.
    expect(badge().className).toContain(badgeTone.neutral);
  });

  it("warns on a trial that is about to end, and still dates it", () => {
    renderTrial(
      orgWith({ trial_state: "ending_soon", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe(`Ending soon${shortDate(TRIAL_ENDS_AT)}`);
    expect(badge().className).toContain(badgeTone.warning);
  });

  it("renders a running trial differently from one that is ending soon", () => {
    renderTrial(
      orgWith({ trial_state: "running", trial_ends_at: TRIAL_ENDS_AT }),
    );
    const running = { text: trialText(), tone: badge().className };
    cleanup();

    renderTrial(
      orgWith({ trial_state: "ending_soon", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).not.toBe(running.text);
    expect(badge().className).not.toBe(running.tone);
  });

  it("reads an expired trial as a failure and drops the date", () => {
    renderTrial(
      orgWith({ trial_state: "expired", trial_ends_at: TRIAL_ENDS_AT }),
    );

    // The date is set on the record and deliberately not shown: a finished
    // trial's end date reads as a deadline still to come.
    expect(trialText()).toBe("Expired");
    expect(badge().className).toContain(badgeTone.destructive);
  });

  it("reads a demoted trial as a failure and drops the date", () => {
    renderTrial(
      orgWith({ trial_state: "demoted", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe("Demoted");
    expect(badge().className).toContain(badgeTone.destructive);
  });

  it("reads a converted trial as a win", () => {
    renderTrial(
      orgWith({ trial_state: "converted", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe("Converted");
    // Success, not destructive. A converted trial is the outcome the business
    // wants, and a red badge sends an operator after a healthy account.
    expect(badge().className).toContain(badgeTone.success);
  });

  it("carries every state on a badge rather than as bare text", () => {
    const states: TrialState[] = [
      "running",
      "ending_soon",
      "expired",
      "demoted",
      "converted",
    ];

    for (const state of states) {
      renderTrial(
        orgWith({ trial_state: state, trial_ends_at: TRIAL_ENDS_AT }),
      );
      // The badge's own element, so text dropped out of a badge fails here
      // even where the words are unchanged. The variant, not the classes it
      // expands to: the tone rides in `className` and would satisfy a class
      // assertion on its own.
      expect(badge().textContent).not.toBe("");
      expect(badge().getAttribute("data-variant")).toBe("outline");
      cleanup();
    }
  });

  it("gives each state its own words, so shared tones stay apart", () => {
    const labels = new Set<string>();
    const states: TrialState[] = [
      "running",
      "ending_soon",
      "expired",
      "demoted",
      "converted",
    ];

    for (const state of states) {
      renderTrial(orgWith({ trial_state: state }));
      labels.add(badge().textContent ?? "");
      cleanup();
    }

    // `expired` and `demoted` share a tone. Their words are the only thing
    // left to tell them apart.
    expect(labels.size).toBe(states.length);
  });
});
