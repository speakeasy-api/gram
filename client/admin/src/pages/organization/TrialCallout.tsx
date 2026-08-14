import type { JSX } from "react";

import type { AdminOrganization, TrialState } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";
import { fmtDateShort } from "@/lib/utils";
import { OrganizationActions } from "@/pages/organizations/OrganizationActions";

// Only while the trial is live. This is not a general-purpose banner slot: an
// expired or demoted trial gets the header badge and nothing else, because
// there is no deadline left to act on.
const LIVE_TRIAL_STATES: ReadonlySet<TrialState> = new Set([
  "running",
  "ending_soon",
]);

export function TrialCallout({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element | null {
  const state = org.trial_state;
  if (!state || !LIVE_TRIAL_STATES.has(state)) return null;

  return (
    <div
      role="status"
      className="border-border bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-md border px-4 py-3"
    >
      <p className="text-sm">
        {org.trial_ends_at ? (
          <>Trial ends {fmtDateShort(org.trial_ends_at)}</>
        ) : (
          // A record the server dates no end for still has a live trial and
          // still takes an extension. The state word carries the callout on its
          // own; `fmtDateShort` would put a bare dash where a deadline goes.
          <>Trial: {TRIAL_LABELS[state]}</>
        )}
      </p>
      {/* A direct child, not wrapped in a spacer. A disabled organization has
          no extendable trial, so this draws nothing, and a wrapper would leave
          a visible gap where the button is not. The rule about when extend is
          offered lives inside these controls, so the callout does not decide it
          a second time. */}
      <OrganizationActions org={org} layout="buttons" actions="trial" />
    </div>
  );
}
