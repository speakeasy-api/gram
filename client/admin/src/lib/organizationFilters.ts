// The vocabulary of the organizations list's filters: the groups an operator
// picks from, and the rules that turn a chosen set into the URL.
//
// It sits in `lib` rather than beside the sheet because the route's search
// schema reads it too, and the sheet is rendered by a page the route imports.
// A group declared in the sheet would close that circle, and a circular import
// leaves whichever module evaluates second holding an undefined constant.

import {
  createdRange,
  type CreatedRange,
  type CreatedRangeKey,
} from "@/lib/createdRange";
import { ACCOUNT_TYPE_OPTIONS, isAccountType } from "@/lib/accountTypes";
import { TRIAL_STATES, type TrialState } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";

// The two restricted states derived from `disabled_at`.
// The API also accepts "all"; statusParams translates these selections.
export const DISABLED_STATES = ["active", "disabled"] as const;

export type DisabledState = (typeof DISABLED_STATES)[number];

export const FILTER_GROUP_KEYS = ["type", "trial", "disabled"] as const;

export type FilterGroupKey = (typeof FILTER_GROUP_KEYS)[number];

export type MemberRange = { minMembers?: string; maxMembers?: string };
export type MemberRangeKey = keyof MemberRange;
export type FilterControlKey =
  | FilterGroupKey
  | MemberRangeKey
  | CreatedRangeKey
  | "created";
/** Chosen values per group; status is empty (All) or a single state. */
export type FilterSelection = Record<FilterGroupKey, string[]> &
  MemberRange &
  CreatedRange;

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

// Shared labels for the dropdown and applied-filter summaries.
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
    label: "Organization Status",
    emptyLabel: "All",
    allLabel: "All",
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

/** Canonical status; old fields are read only for bookmark compatibility. */
export type FilterSearch = MemberRange &
  CreatedRange & {
    type?: string[];
    trial?: TrialState[];
    disabledStatus?: "all" | DisabledState;
    disabled?: DisabledState[];
    disabledOnly?: boolean;
  };

/** New navigations only write the canonical status, omitting All. */
export function filtersToSearch(filters: FilterSelection): MemberRange &
  CreatedRange & {
    type?: string[];
    trial?: TrialState[];
    disabledStatus?: DisabledState;
  } {
  const status = disabledStates(filters.disabled);
  return {
    ...memberRange(filters),
    ...createdRange(filters),
    type: accountTypes(filters.type),
    trial: trialStates(filters.trial),
    disabledStatus: status?.length === 1 ? status[0] : undefined,
  };
}

/** A valid canonical status wins, including explicit All. */
export function statusSelection(search: FilterSearch): string[] {
  if (search.disabledStatus === "all") return [];
  if (
    search.disabledStatus === "active" ||
    search.disabledStatus === "disabled"
  ) {
    return [search.disabledStatus];
  }
  if (search.disabledOnly === true) return ["disabled"];
  if (search.disabledOnly === false) return [];
  return search.disabled?.length === 1 ? search.disabled : [];
}

/** Translate status at the API boundary. */
export function statusParams(search: FilterSearch): {
  disabled_status: "all" | "active" | "disabled";
} {
  const status = statusSelection(search)[0];
  return {
    disabled_status:
      status === "active" || status === "disabled" ? status : "all",
  };
}

const MAX_MEMBERS = "9223372036854775807";
type RawMemberRange = { minMembers?: unknown; maxMembers?: unknown };

// Only strings: Router has already JSON-parsed bare numbers, losing both
// unsafe integer precision and syntax (1e3 / 1.0). Its default serializer
// quotes our decimal strings, so UI-written URLs always round trip losslessly.
function memberBound(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  if (!/^[0-9]+$/.test(trimmed)) return undefined;
  const decimal = trimmed.replace(/^0+(?=\d)/, "");
  if (
    decimal.length > MAX_MEMBERS.length ||
    (decimal.length === MAX_MEMBERS.length && decimal > MAX_MEMBERS)
  )
    return undefined;
  return decimal;
}

export function memberRangeErrors(
  raw: RawMemberRange,
): Partial<Record<MemberRangeKey, string>> {
  const errors: Partial<Record<MemberRangeKey, string>> = {};
  for (const key of ["minMembers", "maxMembers"] as const) {
    const value = raw[key];
    const blank =
      value === undefined || (typeof value === "string" && value.trim() === "");
    if (!blank && memberBound(value) === undefined) {
      errors[key] = `Enter a whole number from 0 to ${MAX_MEMBERS}.`;
    }
  }
  const min = memberBound(raw.minMembers);
  const max = memberBound(raw.maxMembers);
  if (min !== undefined && max !== undefined && BigInt(min) > BigInt(max)) {
    errors.maxMembers = "Max must be greater than or equal to Min.";
  }
  return errors;
}

// Match the route's forgiving filter policy: drop malformed bounds separately;
// drop a reversed pair together, never send an invalid range to the API.
export function memberRange(raw: RawMemberRange): MemberRange {
  const minMembers = memberBound(raw.minMembers);
  const maxMembers = memberBound(raw.maxMembers);
  if (
    minMembers !== undefined &&
    maxMembers !== undefined &&
    BigInt(minMembers) > BigInt(maxMembers)
  ) {
    return { minMembers: undefined, maxMembers: undefined };
  }
  return { minMembers, maxMembers };
}
