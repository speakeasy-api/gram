import type { JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization, TrialState } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";
import { fmtDateShort } from "@/lib/utils";

type TrialDisplay = {
  label: string;
  tone: keyof typeof badgeTone;
  // Only while the trial is still live. A date beside a finished trial reads
  // as a deadline the operator can still act on.
  showsEndDate: boolean;
};

// `neutral` for a running trial and `warning` only for one about to end: the
// server derives that distinction, and giving both the same tone throws it
// away. `expired` and `demoted` share `destructive` because there are four
// tones and five states; their words keep them apart.
//
// The annotation is exhaustive over the union minus `none`, so a seventh state
// is a build failure. It is not the only guard: `Trial.test.tsx` walks
// `TRIAL_STATES` at runtime, so a seventh state fails a test even if this
// annotation is edited away.
const TRIAL_DISPLAY: Record<Exclude<TrialState, "none">, TrialDisplay> = {
  running: {
    label: TRIAL_LABELS.running,
    tone: "neutral",
    showsEndDate: true,
  },
  ending_soon: {
    label: TRIAL_LABELS.ending_soon,
    tone: "warning",
    showsEndDate: true,
  },
  expired: {
    label: TRIAL_LABELS.expired,
    tone: "destructive",
    showsEndDate: false,
  },
  demoted: {
    label: TRIAL_LABELS.demoted,
    tone: "destructive",
    showsEndDate: false,
  },
  converted: {
    label: TRIAL_LABELS.converted,
    tone: "success",
    showsEndDate: false,
  },
};

// Indexed as a plain string record on purpose. The server can start sending a
// state this build has never heard of, and an unknown state has to read as no
// trial rather than crash or put a raw enum name in front of an operator.
const DISPLAY_BY_STATE: Record<string, TrialDisplay | undefined> =
  TRIAL_DISPLAY;

// Own properties only. A plain object literal inherits `Object.prototype`, so
// indexing it with `constructor` or `toString` returns a truthy function and
// the unknown-state branch below never runs, rendering an empty badge with no
// announcement at all. That is the one thing this component must not do.
function displayFor(state: string): TrialDisplay | undefined {
  return Object.hasOwn(TRIAL_DISPLAY, state)
    ? DISPLAY_BY_STATE[state]
    : undefined;
}

// The one account of an organization's trial. The row, the peek panel and the
// detail page all render this, so the same organization cannot read one way in
// the list and another way on its own page.
export function Trial({ org }: { org: AdminOrganization }): JSX.Element {
  const display = displayFor(org.trial_state ?? "");

  if (!display) {
    // A lone hyphen is the entire value of this cell, and a screen reader
    // announces it as nothing at all. It also has to say which of the two
    // silences this is. Reading a state the server derived but this build does
    // not know as "never trialled" is the same laundering the column exists to
    // stop, put back at the edge.
    const unrecognised = Boolean(org.trial_state) && org.trial_state !== "none";
    return (
      <span className="text-muted-foreground text-sm">
        <span aria-hidden="true">-</span>
        <span className="sr-only">
          {unrecognised ? "Trial state not recognised" : TRIAL_LABELS.none}
        </span>
      </span>
    );
  }

  return (
    <span className="flex items-center gap-1.5 text-sm">
      <Badge variant="outline" className={badgeTone[display.tone]}>
        {display.label}
      </Badge>
      {display.showsEndDate && org.trial_ends_at ? (
        <>
          {/* An explicit space, because the flex gap is not in the text. A
              copied cell otherwise reads `Running5/6/2026`. Whitespace-only
              text is not rendered as a flex item, so nothing moves. */}{" "}
          {/* "ends", because the header says `Trial` and no longer says what
              the date is. A bare date beside a state reads as a start. */}
          <span>ends {fmtDateShort(org.trial_ends_at)}</span>
        </>
      ) : null}
    </span>
  );
}
