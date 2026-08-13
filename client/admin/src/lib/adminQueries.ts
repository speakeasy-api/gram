// One queryOptions export per admin endpoint.
//
// Every read and every invalidation goes through these, so a cache key is
// written once. A key spelled out at a call site is the usual cause of a
// mutation that updates the server and leaves the table showing the old row.

import { queryOptions, type QueryKey } from "@tanstack/react-query";
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

export function projectQuery(
  idOrSlug: string,
): AdminQuery<AdminProjectDetail, readonly ["gram-admin-project", string]> {
  return queryOptions({
    queryKey: ["gram-admin-project", idOrSlug] as const,
    queryFn: () => getProject(idOrSlug),
  });
}
