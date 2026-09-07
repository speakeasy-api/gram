import type { TrialState } from "@/lib/gramAdminApi";

// The one wording for each state. The badge on the row and the option that
// filters the list for that state both read from here, so the filter and the
// rows it returns cannot say different words for the same thing.
//
// `none` is included because it is a state an operator filters on, even though
// the cell renders it as a dash rather than a badge.
export const TRIAL_LABELS: Record<TrialState, string> = {
  none: "No trial",
  running: "Running",
  ending_soon: "Ending soon",
  expired: "Expired",
  demoted: "Demoted",
  converted: "Converted",
};
