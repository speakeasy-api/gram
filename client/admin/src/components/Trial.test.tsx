import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Trial } from "@/components/Trial";
import { badgeTone } from "@/lib/badgeTone";
import {
  TRIAL_STATES,
  type AdminOrganization,
  type TrialState,
} from "@/lib/gramAdminApi";

const TRIAL_ENDS_AT = "2026-05-06T00:00:00Z";

const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "pro",
  whitelisted: true,
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

// Walked at runtime, so a seventh state added to `TRIAL_STATES` fails a test
// here whether or not the type annotation in `Trial.tsx` survived the edit
// that added it.
const DATED_STATES = TRIAL_STATES.filter((state) => state !== "none");

function orgWith(trial: Partial<AdminOrganization>): AdminOrganization {
  return { ...ORG, ...trial };
}

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
}

function renderTrial(org: AdminOrganization): void {
  render(
    <div data-testid="trial">
      <Trial org={org} />
    </div>,
  );
}

function cell(): HTMLElement {
  return screen.getByTestId("trial");
}

function trialText(): string {
  return cell().textContent ?? "";
}

// What a sighted operator reads: the sr-only half is stripped, and the
// aria-hidden half is not.
function visibleText(): string {
  return Array.from(cell().querySelectorAll(".sr-only")).reduce(
    (text, hidden) => text.replace(hidden.textContent ?? "", ""),
    trialText(),
  );
}

// What a screen reader announces: the dash is aria-hidden, so it is not part
// of the accessible name and the sr-only text is.
function accessibleText(): string {
  return Array.from(cell().querySelectorAll('[aria-hidden="true"]')).reduce(
    (text, hidden) => text.replace(hidden.textContent ?? "", ""),
    trialText(),
  );
}

function queryBadge(): HTMLElement | null {
  return cell().querySelector<HTMLElement>('[data-slot="badge"]');
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
    expect(visibleText()).toBe("-");
    expect(queryBadge()).toBeNull();
    // A lone hyphen is announced as nothing, and this cell's whole value is
    // that hyphen.
    expect(accessibleText()).toBe("No trial");
  });

  it("reads a dash when the API sends no trial state at all", () => {
    renderTrial(ORG);

    expect(visibleText()).toBe("-");
    expect(queryBadge()).toBeNull();
    expect(accessibleText()).toBe("No trial");
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
    expect(visibleText()).toBe("-");
    expect(queryBadge()).toBeNull();
  });

  it("does not pass an unrecognised state off as no trial", () => {
    renderTrial(orgWith({ trial_state: "paused" as TrialState }));
    const unrecognised = accessibleText();
    cleanup();

    renderTrial(orgWith({ trial_state: "none" }));

    // Both read as a dash, which is right. Calling a state the server derived
    // "no trial" is the laundering this column exists to stop, and a reader
    // who cannot tell the two apart has no way to report the first.
    expect(unrecognised).not.toBe(accessibleText());
    expect(unrecognised).toBe("Trial state not recognised");
  });

  // `TRIAL_DISPLAY` is an object literal, so it inherits `Object.prototype`.
  // Indexing it with one of these names returns a truthy function, and the
  // unknown-state branch is skipped: an empty badge, announcing nothing. The
  // `paused` case above cannot catch it, because `paused` is genuinely absent
  // from the prototype chain.
  it.each(["constructor", "toString", "valueOf", "hasOwnProperty"])(
    "reads the inherited key %s as an unrecognised state",
    (inherited) => {
      renderTrial(
        orgWith({
          trial_state: inherited as TrialState,
          trial_ends_at: TRIAL_ENDS_AT,
        }),
      );

      expect(visibleText()).toBe("-");
      expect(queryBadge()).toBeNull();
      expect(accessibleText()).toBe("Trial state not recognised");
    },
  );

  it("puts the end date beside a running trial and tones it as normal", () => {
    renderTrial(
      orgWith({ trial_state: "running", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe(`Running ends ${shortDate(TRIAL_ENDS_AT)}`);
    // Neutral, not warning: the server has a separate state for a trial about
    // to end, and toning every running trial as urgent wastes it.
    expect(badge().className).toContain(badgeTone.neutral);
  });

  it("warns on a trial that is about to end, and still dates it", () => {
    renderTrial(
      orgWith({ trial_state: "ending_soon", trial_ends_at: TRIAL_ENDS_AT }),
    );

    expect(trialText()).toBe(`Ending soon ends ${shortDate(TRIAL_ENDS_AT)}`);
    expect(badge().className).toContain(badgeTone.warning);
  });

  it("says the date is an end date, and separates it from the state", () => {
    renderTrial(
      orgWith({ trial_state: "running", trial_ends_at: TRIAL_ENDS_AT }),
    );

    // The header reads `Trial` and no longer says what the date is, so the
    // cell has to. The space is in the text, not only in the flex gap: a
    // copied cell otherwise reads `Running5/6/2026`.
    expect(trialText()).toContain(` ends ${shortDate(TRIAL_ENDS_AT)}`);
    expect(trialText()).not.toContain(`Running${shortDate(TRIAL_ENDS_AT)}`);
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

  it.each(DATED_STATES)(
    "shows no date for %s where the record carries none",
    (state) => {
      renderTrial(orgWith({ trial_state: state, trial_ends_at: undefined }));

      // A missing factual date must not produce a trailing bare `ends` or
      // `Running-`; there is no fallback date for this state.
      expect(trialText()).not.toContain("ends");
      expect(trialText()).not.toContain("-");
      expect(trialText()).toBe(badge().textContent);
    },
  );

  it.each(DATED_STATES)(
    "carries %s on a badge rather than as bare text",
    (state) => {
      renderTrial(
        orgWith({ trial_state: state, trial_ends_at: TRIAL_ENDS_AT }),
      );

      // The badge's own element, so text dropped out of a badge fails here even
      // where the words are unchanged. The variant, not the classes it expands
      // to: the tone rides in `className` and would satisfy a class assertion on
      // its own.
      expect(badge().textContent).not.toBe("");
      expect(badge().getAttribute("data-variant")).toBe("outline");
    },
  );

  it("gives each state its own words, so shared tones stay apart", () => {
    const labels = new Set<string>();

    for (const state of DATED_STATES) {
      renderTrial(orgWith({ trial_state: state }));
      labels.add(badge().textContent ?? "");
      cleanup();
    }

    // `expired` and `demoted` share a tone. Their words are the only thing
    // left to tell them apart.
    expect(labels.size).toBe(DATED_STATES.length);
  });
});
