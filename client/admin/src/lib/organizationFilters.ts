// The vocabulary of the organizations list's filters: the groups an operator
// picks from, and the rules that turn a chosen set into the URL.
//
// It sits in `lib` rather than beside the sheet because the route's search
// schema reads it too, and the sheet is rendered by a page the route imports.
// A group declared in the sheet would close that circle, and a circular import
// leaves whichever module evaluates second holding an undefined constant.

import { ACCOUNT_TYPE_OPTIONS, isAccountType } from "@/lib/accountTypes";
import { TRIAL_STATES, type TrialState } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";

// Whether an organization is switched off. Declared here rather than in the
// API module because the server has no enum for it: it derives the state from
// `disabled_at`, and these two words are the whole of what `disabled_states`
// accepts.
export const DISABLED_STATES = ["active", "disabled"] as const;

export type DisabledState = (typeof DISABLED_STATES)[number];

export const FILTER_GROUP_KEYS = ["type", "trial", "disabled"] as const;

export type FilterGroupKey = (typeof FILTER_GROUP_KEYS)[number];

/** One list of chosen values per group, in the order the pickers offer them. */
export type FilterSelection = Record<FilterGroupKey, string[]>;

export const NO_FILTERS: FilterSelection = {
  type: [],
  trial: [],
  disabled: [],
};

export type FilterOption = { value: string; label: string };

export type FilterGroup = {
  key: FilterGroupKey;
  label: string;
  // What the group filters on when nothing is chosen. Not every default is
  // "everything", and an operator cannot be expected to know which is which,
  // so each group says its own.
  emptyLabel: string;
  // What every value at once amounts to, where that is worth naming. Absent on
  // Type: an organization can carry a type the picker does not offer, so
  // choosing all three still leaves rows out.
  allLabel?: string;
  options: FilterOption[];
};

// "Status", rather than the "disabled" the parameter is named for: a group
// headed Disabled whose first option is Active reads as a contradiction.
const DISABLED_LABELS: Record<DisabledState, string> = {
  active: "Active",
  disabled: "Disabled",
};

// The order here is the order the URL, the request and the cache key put a
// chosen set into, because everything below sorts back into it.
export const FILTER_GROUPS: FilterGroup[] = [
  {
    key: "type",
    label: "Type",
    emptyLabel: "All types",
    options: ACCOUNT_TYPE_OPTIONS.map((value) => ({ value, label: value })),
  },
  {
    key: "trial",
    label: "Trial",
    emptyLabel: "All trial states",
    // Every organization has exactly one of these, `none` included.
    allLabel: "All trial states",
    // TRIAL_LABELS is the map the Trial cell renders its badge from, so the
    // filter and the rows it returns cannot say different words for one state.
    options: TRIAL_STATES.map((value) => ({
      value,
      label: TRIAL_LABELS[value],
    })),
  },
  {
    key: "disabled",
    label: "Status",
    emptyLabel: "Active only",
    // Not the empty label: this group's default is a filter, not everything.
    allLabel: "Active and disabled",
    options: DISABLED_STATES.map((value) => ({
      value,
      label: DISABLED_LABELS[value],
    })),
  },
];

/**
 * What a group's control says it is filtering on. The one chosen value is
 * named rather than counted, because "1 selected" makes an operator open the
 * sheet to learn what they already decided.
 */
export function filterSummary(
  group: FilterGroup,
  chosen: string[],
  options: FilterOption[] = group.options,
): string {
  if (chosen.length === 0) return group.emptyLabel;
  const only = chosen[0];
  if (chosen.length === 1 && only !== undefined) {
    return options.find((option) => option.value === only)?.label ?? only;
  }
  if (
    group.allLabel !== undefined &&
    group.options.every((option) => chosen.includes(option.value))
  ) {
    return group.allLabel;
  }
  return `${chosen.length} selected`;
}

/**
 * The options a group offers, given what is already chosen. Only the account
 * types can differ from the declared list: an organization can carry a type
 * from outside it, so a link that filters on one has to show that value as a
 * chosen option rather than drop it and read as unfiltered.
 */
export function optionsFor(
  group: FilterGroup,
  chosen: string[],
): FilterOption[] {
  const declared = new Set(group.options.map((option) => option.value));
  const extra = chosen
    .filter((value) => !declared.has(value))
    .map((value) => ({ value, label: value }));
  return extra.length > 0 ? [...group.options, ...extra] : group.options;
}

/**
 * Kept in the options' order rather than the order the operator clicked, so
 * the URL a chosen set produces does not depend on the path taken to it.
 */
export function toggleFilter(
  chosen: string[],
  value: string,
  options: FilterOption[],
): string[] {
  const next = new Set(chosen);
  if (!next.delete(value)) next.add(value);
  return options.map((option) => option.value).filter((item) => next.has(item));
}

// Ordered by the list the picker offers rather than by the order a link
// happened to name, and deduplicated. Two operators who chose the same filter
// in a different order then send the same request, which is the same cache
// entry and the same pager signature.
function inOptionOrder<T extends string>(
  chosen: string[],
  options: readonly T[],
): T[] {
  const wanted = new Set(chosen);
  return options.filter((option) => wanted.has(option));
}

/**
 * Unlike the two below, an unrecognised value is kept. `ACCOUNT_TYPE_OPTIONS`
 * is the list the picker offers, not the list the column can hold. Dropping a
 * value from outside it would widen a link's view without saying so, and leave
 * the picker reading "all types" while the rows were still filtered.
 */
export function accountTypes(chosen: string[]): string[] | undefined {
  const known = inOptionOrder(chosen, ACCOUNT_TYPE_OPTIONS);
  const rest = [...new Set(chosen.filter((item) => !isAccountType(item)))];
  const all = [...known, ...rest];
  return all.length > 0 ? all : undefined;
}

/**
 * A state the server does not derive is dropped. Unlike an account type it can
 * match no row at all, so keeping it would empty a list whose control claimed
 * the state was on.
 */
export function trialStates(chosen: string[]): TrialState[] | undefined {
  const kept = inOptionOrder(chosen, TRIAL_STATES);
  return kept.length > 0 ? kept : undefined;
}

export function disabledStates(chosen: string[]): DisabledState[] | undefined {
  const kept = inOptionOrder(chosen, DISABLED_STATES);
  return kept.length > 0 ? kept : undefined;
}

/** The three params a chosen set puts in the URL. */
export type FilterSearch = {
  type?: string[];
  trial?: TrialState[];
  disabled?: DisabledState[];
};

/**
 * A chosen set as the URL states it.
 *
 * Written through the same three readers a pasted link goes through, so a
 * control cannot put a value in the URL that a reload would refuse: the view
 * an operator sends is the view they are looking at.
 */
export function filtersToSearch(filters: FilterSelection): FilterSearch {
  return {
    type: accountTypes(filters.type),
    trial: trialStates(filters.trial),
    disabled: disabledStates(filters.disabled),
  };
}
