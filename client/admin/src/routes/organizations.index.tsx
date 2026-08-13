import { createFileRoute } from "@tanstack/react-router";

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
  type?: string[];
  trial?: string;
  disabled?: boolean;
  sort?: string;
  dir?: "asc" | "desc";
  /** 1-based. Declared, not yet wired: the list API is still cursor-paged. */
  page?: number;
};

// The router decodes `?q=123` to the number 123 before this runs, so a numeric
// search term arrives typed as a number rather than as text.
function text(value: unknown): string | undefined {
  if (typeof value === "number") return String(value);
  if (typeof value === "string" && value !== "") return value;
  return undefined;
}

function textArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const items = value.filter(
    (item): item is string => typeof item === "string",
  );
  return items.length > 0 ? items : undefined;
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
    type: textArray(search["type"]),
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
