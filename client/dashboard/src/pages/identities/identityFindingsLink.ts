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

// The filter lives in the URL so the Security summaries can link straight to
// a slice of the list, and a filtered view can be shared.
export const CATEGORY_PARAM = "category";
export const RULE_PARAM = "rule";

/**
 * The Findings sub-page for this identity, narrowed to `filter`. Keeps the
 * reader's window and other params.
 */
export function identityFindingsHref(
  routes: ReturnType<typeof useRoutes>,
  urn: string,
  search: string,
  filter: FindingsFilter,
): string {
  const params = new URLSearchParams(search);
  params.delete(CATEGORY_PARAM);
  params.delete(RULE_PARAM);
  if (filter.category) params.set(CATEGORY_PARAM, filter.category);
  if (filter.ruleId) params.set(RULE_PARAM, filter.ruleId);
  const query = params.toString();
  const base = routes.identities.detail.findings.href(encodeIdentityUrn(urn));
  return query ? `${base}?${query}` : base;
}
