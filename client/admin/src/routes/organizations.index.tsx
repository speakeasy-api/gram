import { createFileRoute } from "@tanstack/react-router";

import { isAccountType, type AccountType } from "@/lib/accountTypes";
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
 * field is optional.
 */
export type OrganizationsSearch = {
  q?: string;
  type?: AccountType;
  trial?: string;
  disabled?: boolean;
  sort?: string;
  dir?: "asc" | "desc";
  /** 1-based. Declared, not yet wired: the list API is still cursor-paged. */
  page?: number;
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

// Anything outside the accepted list is dropped, so the URL, the control and
// the request agree that no type filter is on. Keeping an unknown value would
// filter the rows while the control read empty.
//
// One value rather than a list: the API takes a single account_type and the
// control offers a single choice, so a list would let a link advertise more
// than the request sends.
function accountType(value: unknown): AccountType | undefined {
  const item = text(value);
  return isAccountType(item) ? item : undefined;
}

function flag(value: unknown): true | undefined {
  return value === true || value === "true" ? true : undefined;
}

function direction(value: unknown): "asc" | "desc" | undefined {
  if (value === "asc") return "asc";
  if (value === "desc") return "desc";
  return undefined;
}

// Page 1 is the default, so it never reaches the URL.
function pageNumber(value: unknown): number | undefined {
  const page = typeof value === "number" ? value : Number(value);
  return Number.isInteger(page) && page > 1 ? page : undefined;
}

export function organizationsSearchSchema(
  search: Record<string, unknown>,
): OrganizationsSearch {
  return {
    q: text(search["q"]),
    type: accountType(search["type"]),
    trial: text(search["trial"]),
    disabled: flag(search["disabled"]),
    sort: text(search["sort"]),
    dir: direction(search["dir"]),
    page: pageNumber(search["page"]),
  };
}

export const Route = createFileRoute("/organizations/")({
  component: OrganizationsList,
  validateSearch: organizationsSearchSchema,
});
