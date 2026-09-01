// One queryOptions export per admin endpoint.
//
// Every read and every invalidation goes through these, so a cache key is
// written once. A key spelled out at a call site is the usual cause of a
// mutation that updates the server and leaves the table showing the old row.

import {
  infiniteQueryOptions,
  queryOptions,
  type InfiniteData,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import {
  getOrganization,
  getOrganizationChatAnalysisSettings,
  getOrganizationFeatures,
  getOrganizationStats,
  getInferenceKeys,
  getInferenceSpendHistory,
  getPaygBillingSummary,
  getStripeSubscription,
  getProject,
  getSession,
  listOrganizationActivity,
  listOrganizationMembers,
  listOrganizationProjects,
  listOrganizations,
  omitUnset,
  type AdminInferenceKey,
  type AdminInferenceSpendMonth,
  type AdminOrganization,
  type AdminOrganizationChatAnalysisSettings,
  type AdminOrganizationFeatures,
  type AdminProjectDetail,
  type AdminPaygBillingSummary,
  type AdminStripeSubscription,
  type ListOrganizationActivityResult,
  type ListOrganizationMembersResult,
  type ListOrganizationProjectsResult,
  type ListOrganizationsParams,
  type ListOrganizationsResult,
} from "@/lib/gramAdminApi";

// What queryOptions infers, named so the exports can carry the return type that
// `typescript/explicit-module-boundary-types` demands. Writing the shape out by
// hand instead would drop the tag queryOptions puts on queryKey, and that tag
// is what tells setQueryData which payload the key holds.
//
// TKey has to be the exact tuple. Collapsing it to QueryKey fails to compile,
// because `enabled` takes a Query<..., TKey> and that position is
// contravariant. The `as const` on each key exists to produce that tuple.
type AdminQuery<TData, TKey extends QueryKey> = ReturnType<
  typeof queryOptions<TData, Error, TData, TKey>
>;

// The backend ends the session, never the client, so staleTime keeps a good
// session from refetching. A failed check holds no data, which React Query
// always treats as stale, so it still retries on focus and on reconnect.
export const adminSessionQuery = queryOptions({
  queryKey: ["adminSession"] as const,
  queryFn: getSession,
  staleTime: Infinity,
});

// Call with no argument for the key that covers every filter and every page:
// invalidateQueries({ queryKey: organizationsListQuery().queryKey }).
export function organizationsListQuery(
  params: ListOrganizationsParams = {},
): AdminQuery<
  ListOrganizationsResult,
  readonly ["gram-admin-organizations", ListOrganizationsParams]
> {
  return queryOptions({
    queryKey: ["gram-admin-organizations", omitUnset(params)] as const,
    queryFn: () => listOrganizations(params),
  });
}

// A constant, not a function: with no parameters there is no key to move, so
// filtering the table cannot make these totals track the rows on screen.
export const organizationsStatsQuery = queryOptions({
  queryKey: ["gram-admin-organization-stats"] as const,
  queryFn: getOrganizationStats,
});

// Named once, because the detail entry is reached two ways. The route takes an
// id or a slug and each is its own entry, so this is the only thing the two
// have in common and the only way to name both at once.
const ORGANIZATION_KEY = "gram-admin-organization";

export function organizationQuery(
  idOrSlug: string,
): AdminQuery<AdminOrganization, readonly ["gram-admin-organization", string]> {
  return queryOptions({
    queryKey: [ORGANIZATION_KEY, idOrSlug] as const,
    queryFn: () => getOrganization(idOrSlug),
  });
}

type OrganizationActivityQuery = ReturnType<
  typeof infiniteQueryOptions<
    ListOrganizationActivityResult,
    Error,
    InfiniteData<ListOrganizationActivityResult, string | undefined>,
    readonly ["gram-admin-organization-activity", string],
    string | undefined
  >
>;

export function organizationActivityQuery(
  organizationID: string,
): OrganizationActivityQuery {
  return infiniteQueryOptions({
    queryKey: ["gram-admin-organization-activity", organizationID] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      listOrganizationActivity(organizationID, pageParam),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  });
}

export function invalidateOrganizationActivity(
  qc: QueryClient,
  organizationID: string,
): void {
  void qc.invalidateQueries({
    queryKey: organizationActivityQuery(organizationID).queryKey,
  });
}

export function organizationFeaturesQuery(
  organizationID: string,
): AdminQuery<
  AdminOrganizationFeatures,
  readonly ["gram-admin-organization-features", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-organization-features", organizationID] as const,
    queryFn: () => getOrganizationFeatures(organizationID),
  });
}

export function organizationChatAnalysisSettingsQuery(
  organizationID: string,
): AdminQuery<
  AdminOrganizationChatAnalysisSettings,
  readonly ["gram-admin-organization-chat-analysis-settings", string]
> {
  return queryOptions({
    queryKey: [
      "gram-admin-organization-chat-analysis-settings",
      organizationID,
    ] as const,
    queryFn: () => getOrganizationChatAnalysisSettings(organizationID),
  });
}

export function organizationProjectsQuery(
  organizationID: string,
): AdminQuery<
  ListOrganizationProjectsResult,
  readonly ["gram-admin-organization-projects", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-organization-projects", organizationID] as const,
    queryFn: () => listOrganizationProjects(organizationID),
  });
}

export function organizationMembersQuery(
  organizationID: string,
): AdminQuery<
  ListOrganizationMembersResult,
  readonly ["gram-admin-organization-members", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-organization-members", organizationID] as const,
    queryFn: () => listOrganizationMembers(organizationID),
  });
}

export function inferenceKeysQuery(
  organizationID: string,
): AdminQuery<
  AdminInferenceKey[],
  readonly ["gram-admin-inference-keys", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-inference-keys", organizationID] as const,
    queryFn: () => getInferenceKeys(organizationID),
    retry: false,
  });
}

export function inferenceSpendHistoryQuery(
  organizationID: string,
): AdminQuery<
  AdminInferenceSpendMonth[],
  readonly ["gram-admin-inference-spend-history", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-inference-spend-history", organizationID] as const,
    queryFn: () => getInferenceSpendHistory(organizationID),
    retry: false,
  });
}

export function paygBillingSummaryQuery(
  organizationID: string,
): AdminQuery<
  AdminPaygBillingSummary,
  readonly ["gram-admin-payg-billing-summary", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-payg-billing-summary", organizationID] as const,
    queryFn: () => getPaygBillingSummary(organizationID),
    retry: false,
  });
}

export function stripeSubscriptionQuery(
  organizationID: string,
): AdminQuery<
  AdminStripeSubscription,
  readonly ["gram-admin-stripe-subscription", string]
> {
  return queryOptions({
    queryKey: ["gram-admin-stripe-subscription", organizationID] as const,
    queryFn: () => getStripeSubscription(organizationID),
    retry: false,
  });
}

export function invalidateOrganizationBilling(
  qc: QueryClient,
  organizationID: string,
): Promise<void> {
  return Promise.all([
    qc.invalidateQueries({
      queryKey: paygBillingSummaryQuery(organizationID).queryKey,
    }),
    qc.invalidateQueries({
      queryKey: stripeSubscriptionQuery(organizationID).queryKey,
    }),
  ]).then(() => undefined);
}

// Every cache a write to an organization can stale, listed once. Cancelling and
// invalidating read the same list, so a key added here reaches both: a key added
// to one of them alone is the stale row this file exists to prevent.
const ORGANIZATION_CACHES: readonly QueryKey[] = [
  organizationsListQuery().queryKey,
  // The whole detail key rather than one organization's, and that is the point
  // rather than laziness. `writeOrganizationToCache` writes the record under its
  // id and under its slug, the row links by slug wherever there is one, and the
  // slug is not known at the moment a write goes out: it arrives with the
  // response the write has not made yet. Naming the id alone would leave the
  // entry the operator actually opened free to answer late and put the record
  // back.
  //
  // Touching another organization's detail entry costs that page a refetch and
  // nothing else, and only one detail query is ever in flight from here.
  [ORGANIZATION_KEY],
  // Measured: an invalidation cannot refire a stats fetch that is already
  // running. React Query joins the open request instead of starting a second
  // one, and its pre-write answer clears the invalidated flag as it lands. So
  // the cancel has to come first, and `invalidateOrganizationStats` covers the
  // write paths that end without one.
  organizationsStatsQuery.queryKey,
];

// Called before a write goes out, because a read already in flight outlives it.
// React Query commits whatever a request answers whenever it lands, so a list
// fetch that started before the write returns the row in its old state
// afterwards and undoes what the write just put in the cache. The row then
// reads as though the write never happened, until something fetches that page
// again.
//
// That window is ordinary rather than exotic: the list query sets no staleTime,
// so returning to the tab starts a refetch, and the operator's next press lands
// while it is still open.
//
// Cancelling rather than awaiting, because the answer is already stale: it was
// asked before the write the operator just made.
export function cancelOrganizationFetches(qc: QueryClient): Promise<void> {
  return Promise.all(
    ORGANIZATION_CACHES.map((queryKey) => qc.cancelQueries({ queryKey })),
  ).then(() => undefined);
}

// Every admin write answers with the organization in its new state, so paged
// list caches are written from the response. Refetching a filtered list can
// move the row out from under the operator who just acted on it; the detail
// cache is separately invalidated after this immediate repaint.
//
// One consequence, accepted rather than overlooked: the default list request
// sends no disabled_states, which asks for active organizations only, so a row
// that has just been disabled keeps its place on a page whose filter no longer
// describes it, until something else fetches that page. The alternative is
// dropping the row from under the operator the moment they act on it, which is
// worse.
//
// It lives beside the keys rather than at a call site for the reason at the top
// of this file: a key spelled out by hand is how a write updates the server and
// leaves the table showing the old row.
export function writeOrganizationToCache(
  qc: QueryClient,
  org: AdminOrganization,
): void {
  // The detail route takes either, and each is its own entry.
  qc.setQueryData(organizationQuery(org.id).queryKey, org);
  if (org.slug) qc.setQueryData(organizationQuery(org.slug).queryKey, org);

  // Every filter and every page, because the operator can act on a row from any
  // of them and the rest stay in the cache behind it.
  //
  // The early return saves the rebuild on a page the record is not on. It is
  // an allocation, not a correctness property: React Query hands back the old
  // reference for a deeply equal result either way, so a page that never held
  // the record keeps its identity with or without this.
  qc.setQueriesData<ListOrganizationsResult>(
    { queryKey: organizationsListQuery().queryKey },
    (previous) => {
      if (!previous?.organizations.some((row) => row.id === org.id)) {
        return previous;
      }
      return {
        ...previous,
        organizations: previous.organizations.map((row) =>
          row.id === org.id ? org : row,
        ),
      };
    },
  );

  // Refetched, not written: the response holds one record and these are counts
  // over all of them.
  invalidateOrganizationStats(qc);
}

// Keep the mutation response in paged lists so an acted-on row stays put, but
// ask the canonical detail endpoint for the record again. Both route addresses
// are invalidated because the active page may have been opened by id or slug.
export function invalidateOrganizationDetails(
  qc: QueryClient,
  org: AdminOrganization,
): void {
  void qc.invalidateQueries({
    queryKey: organizationQuery(org.id).queryKey,
    exact: true,
  });
  if (org.slug) {
    void qc.invalidateQueries({
      queryKey: organizationQuery(org.slug).queryKey,
      exact: true,
    });
  }
}

/**
 * The other half of the cancel above, for a write that never lands. The stats
 * read it dropped has nothing to replace it, and a first read cancelled before
 * its answer holds no figures to fall back on, so the strip keeps three dashes
 * until a focus or a remount asks again.
 *
 * Not awaited anywhere: the counts are the last thing on the page to matter.
 */
export function invalidateOrganizationStats(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: organizationsStatsQuery.queryKey });
}

// For a write that answers with ids rather than records, so there is nothing to
// put in the cache. Every key and the whole of each: the operator can act on
// rows from any page under any filter.
export function invalidateOrganizations(qc: QueryClient): Promise<void> {
  return Promise.all(
    ORGANIZATION_CACHES.map((queryKey) => qc.invalidateQueries({ queryKey })),
  ).then(() => undefined);
}

// The organization is part of the key, not just the request: the same slug names
// a different project in each one, so two organizations must not share a cache
// entry. It is the route's own address for the organization, id or slug as
// typed, because the breadcrumb builds this key from route params alone and can
// never know the resolved id.
export function projectQuery(
  idOrSlug: string,
  organizationIdOrSlug?: string,
): AdminQuery<
  AdminProjectDetail,
  readonly ["gram-admin-project", string, string | null]
> {
  return queryOptions({
    queryKey: [
      "gram-admin-project",
      idOrSlug,
      organizationIdOrSlug ?? null,
    ] as const,
    queryFn: () => getProject(idOrSlug, organizationIdOrSlug),
  });
}
