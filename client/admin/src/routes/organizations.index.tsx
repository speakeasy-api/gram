import { createFileRoute } from "@tanstack/react-router";

import {
  accountTypes,
  memberRange,
  disabledStates,
  DISABLED_STATES,
  trialStates,
  statusSelection,
  type DisabledState,
} from "@/lib/organizationFilters";
import { type TrialState } from "@/lib/gramAdminApi";
import { OrganizationsList } from "@/pages/organizations/index";

/**
 * The organizations list keeps its state in the URL, so an operator can paste
 * the view they are looking at to a colleague and get the same rows back.
 *
 * The names below are the contract between the controls that write them, the
 * request the query layer sends, and any link that carries them. They are
 * expensive to rename once links exist, so they are declared in full here even
 * though this slice only sends some of them.
 *
 * A param is absent from the URL whenever it holds its default. That keeps a
 * shared link down to what the operator actually changed, and it is why every
 * field is optional. Empty sets and disabledStatus=all are absent; the API
 * mapping sends disabled_status=all for unrestricted views.
 */
export type OrganizationsSearch = {
  q?: string;
  minMembers?: string;
  maxMembers?: string;
  type?: string[];
  trial?: TrialState[];
  disabledStatus?: DisabledState;
  /** Read-only aliases are cleared after validation and never written. */
  disabled?: never;
  disabledOnly?: never;
  sort?: string;
  dir?: "asc" | "desc";
};

// The router parses a param that reads as a JSON literal before this runs, so
// a hand-written `?q=123` arrives as a number, `?q=true` as a boolean and
// `?q=null` as null. All three are terms someone can put in a link, so all
// three come back as text. A list or an object is not a term, and is dropped.
function text(value: unknown): string | undefined {
  if (
    typeof value === "number" ||
    typeof value === "boolean" ||
    value === null
  ) {
    return String(value);
  }
  // This is the one place that normalises the term. `?q=acme%20` and `?q=acme`
  // have to reach the API alike, or they are two cache entries holding the same
  // rows, and a term that is only whitespace must reach neither the control nor
  // the request.
  if (typeof value === "string") return value.trim() || undefined;
  return undefined;
}

// A filter is a set, and the router hands it over as a list only when the link
// was written as one: `?type=["free","pro"]` arrives as an array and `?type=pro`
// as a bare string. A person typing a single value into the address bar writes
// the second, so both are read as a set of one or more.
function values(value: unknown): string[] {
  const items = Array.isArray(value) ? (value as unknown[]) : [value];
  return items.map(text).filter((item): item is string => item !== undefined);
}

// `?disabled=true` is the parameter this list used to carry, and it meant
// "disabled organizations as well as active ones". A bookmark holding it has to
// keep showing both, or the link quietly narrows to this filter's default.
function statuses(value: unknown): DisabledState[] | undefined {
  if (value === true || value === "true") return [...DISABLED_STATES];
  return disabledStates(values(value));
}

function direction(value: unknown): "asc" | "desc" | undefined {
  if (value === "asc") return "asc";
  if (value === "desc") return "desc";
  return undefined;
}

export function organizationsSearchSchema(
  search: Record<string, unknown>,
): OrganizationsSearch {
  const only = search["disabledOnly"];
  // A valid binary value wins over the older disabled field, including false.
  const explicit =
    only === true || only === false || only === "true" || only === "false";
  const canonical = search["disabledStatus"];
  const disabledStatus = statusSelection({
    disabledStatus:
      canonical === "all" || canonical === "active" || canonical === "disabled"
        ? canonical
        : undefined,
    disabledOnly: explicit ? only === true || only === "true" : undefined,
    disabled: statuses(search["disabled"]),
  })[0] as DisabledState | undefined;
  return {
    ...memberRange(search),
    disabledStatus,
    // Router search merges validated fields with raw fields. Clear read aliases
    // explicitly so invalid/raw values cannot reappear or survive new writes.
    disabled: undefined,
    disabledOnly: undefined,
    q: text(search["q"]),
    type: accountTypes(values(search["type"])),
    trial: trialStates(values(search["trial"])),
    sort: text(search["sort"]),
    dir: direction(search["dir"]),
  };
}

export const Route = createFileRoute("/organizations/")({
  component: OrganizationsList,
  validateSearch: organizationsSearchSchema,
});
