import type { JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization, TrialState } from "@/lib/gramAdminApi";
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
// Exhaustive over the union minus `none`, so a seventh state added to
// `TrialState` is a build failure here rather than a silent dash.
const TRIAL_DISPLAY = {
  running: { label: "Running", tone: "neutral", showsEndDate: true },
  ending_soon: { label: "Ending soon", tone: "warning", showsEndDate: true },
  expired: { label: "Expired", tone: "destructive", showsEndDate: false },
  demoted: { label: "Demoted", tone: "destructive", showsEndDate: false },
  converted: { label: "Converted", tone: "success", showsEndDate: false },
} as const satisfies Record<Exclude<TrialState, "none">, TrialDisplay>;

// Indexed as a plain string record on purpose. The server can start sending a
// state this build has never heard of, and an unknown state has to read as no
// trial rather than crash or put a raw enum name in front of an operator.
const DISPLAY_BY_STATE: Record<string, TrialDisplay | undefined> =
  TRIAL_DISPLAY;

// The one account of an organization's trial. The row, the peek panel and the
// detail page all render this, so the same organization cannot read one way in
// the list and another way on its own page.
export function Trial({ org }: { org: AdminOrganization }): JSX.Element {
  const display = DISPLAY_BY_STATE[org.trial_state ?? ""];

  if (!display) {
    return <span className="text-muted-foreground text-sm">-</span>;
  }

  return (
    <span className="flex items-center gap-1.5 text-sm">
      <Badge variant="outline" className={badgeTone[display.tone]}>
        {display.label}
      </Badge>
      {display.showsEndDate && org.trial_ends_at
        ? fmtDateShort(org.trial_ends_at)
        : null}
    </span>
  );
}
