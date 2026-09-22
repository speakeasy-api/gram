import { encodeIdentityUrn } from "@/lib/identity-urn";
import type { useRoutes } from "@/routes";

/** What the list is narrowed to. A rule filter replaces a category filter. */
export type FindingsFilter = { category?: string; ruleId?: string };

/**
 * Risk and the shadow inventory are org:admin surfaces and their queries are
 * held back without it. Said outright, because the alternative rendering — the
 * panel's own empty state — reads as "we looked and there is nothing".
 */
export const RISK_UNAVAILABLE =
  "Risk and shadow MCP findings need the org:admin permission.";

export const CATEGORY_PARAM = "category";
export const RULE_PARAM = "rule";

/** Writes `filter` into `params`; a rule filter drops the category. */
export function setFindingsFilterParams(
  params: URLSearchParams,
  filter: FindingsFilter,
): URLSearchParams {
  params.delete(CATEGORY_PARAM);
  params.delete(RULE_PARAM);
  if (filter.ruleId) params.set(RULE_PARAM, filter.ruleId);
  else if (filter.category) params.set(CATEGORY_PARAM, filter.category);
  return params;
}

/** `search` without a Findings filter, as a `?`-prefixed string or "". */
export function withoutFindingsFilter(search: string): string {
  const query = setFindingsFilterParams(
    new URLSearchParams(search),
    {},
  ).toString();
  return query ? `?${query}` : "";
}

/** This identity's Findings page, narrowed to `filter`, keeping other params. */
export function identityFindingsHref(
  routes: ReturnType<typeof useRoutes>,
  urn: string,
  search: string,
  filter: FindingsFilter,
): string {
  const query = setFindingsFilterParams(
    new URLSearchParams(search),
    filter,
  ).toString();
  const base = routes.identities.detail.findings.href(encodeIdentityUrn(urn));
  return query ? `${base}?${query}` : base;
}
