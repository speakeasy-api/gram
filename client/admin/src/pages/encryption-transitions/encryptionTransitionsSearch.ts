import { z } from "zod";

import { TRANSITION_STATES } from "@/lib/organizationEncryption";

/** Which organizations the list shows. */
export const VALUES_FILTERS = ["present", "all"] as const;
export type ValuesFilter = (typeof VALUES_FILTERS)[number];

/** The list shows only organizations with values unless asked for all. */
export const DEFAULT_VALUES: ValuesFilter = "present";

export const VALUES_FILTER_LABELS: Record<ValuesFilter, string> = {
  present: "With values",
  all: "All",
};

/** How the list is ordered. */
export const TRANSITION_SORTS = ["last_written", "oldest"] as const;
export type TransitionSort = (typeof TRANSITION_SORTS)[number];

/** Recently written organizations come first unless asked otherwise. */
export const DEFAULT_SORT: TransitionSort = "last_written";

export const TRANSITION_SORT_LABELS: Record<TransitionSort, string> = {
  last_written: "Last written first",
  oldest: "Oldest organization first",
};

const searchSchema = z.object({
  // The use case listed. Absent lists the first one the server reports.
  useCase: z.string().min(1).optional().catch(undefined),
  // Only organizations in this transition state; absent lists every state.
  state: z.enum(TRANSITION_STATES).optional().catch(undefined),
  // Absent is DEFAULT_VALUES.
  values: z.enum(VALUES_FILTERS).optional().catch(undefined),
  // Absent is DEFAULT_SORT.
  sort: z.enum(TRANSITION_SORTS).optional().catch(undefined),
  page: z.coerce.number().int().min(0).optional().catch(undefined),
});

export type EncryptionTransitionsSearch = z.infer<typeof searchSchema>;

export function encryptionTransitionsSearch(
  search: Record<string, unknown>,
): EncryptionTransitionsSearch {
  return searchSchema.parse(search);
}
