// One queryOptions export per admin endpoint.
//
// Every read and every invalidation goes through these, so a cache key is
// written once. A key spelled out at a call site is the usual cause of a
// mutation that updates the server and leaves the table showing the old row.

import {
  queryOptions,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import {
  getOrganization,
  getProject,
  getSession,
  listOrganizationMembers,
  listOrganizationProjects,
  listOrganizations,
  omitUnset,
  type AdminOrganization,
  type AdminProjectDetail,
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

export function organizationQuery(
  idOrSlug: string,
): AdminQuery<AdminOrganization, readonly ["gram-admin-organization", string]> {
  return queryOptions({
    queryKey: ["gram-admin-organization", idOrSlug] as const,
    queryFn: () => getOrganization(idOrSlug),
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
export function cancelOrganizationFetches(
  qc: QueryClient,
  id: string,
): Promise<void> {
  return Promise.all([
    qc.cancelQueries({ queryKey: organizationsListQuery().queryKey }),
    qc.cancelQueries({ queryKey: organizationQuery(id).queryKey }),
  ]).then(() => undefined);
}

// Every admin write answers with the organization in its new state, so the
// caches that hold that record are written from the response. A refetch would
// be the alternative, and the list is cursor-paged and filtered: refetching it
// can move the row out from under the operator who just acted on it.
//
// One consequence, accepted rather than overlooked: the default list request
// omits include_disabled, so a row that has just been disabled keeps its place
// on a page whose filter no longer describes it, until something else fetches
// that page. The alternative is dropping the row from under the operator the
// moment they act on it, which is worse.
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
}

export function projectQuery(
  idOrSlug: string,
): AdminQuery<AdminProjectDetail, readonly ["gram-admin-project", string]> {
  return queryOptions({
    queryKey: ["gram-admin-project", idOrSlug] as const,
    queryFn: () => getProject(idOrSlug),
  });
}
