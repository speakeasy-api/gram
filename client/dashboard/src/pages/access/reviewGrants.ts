/**
 * Pure helpers behind the review step and the assignment preview: what a
 * save or an assignment adds, and which of it is sensitive. Everything is
 * read off the catalogue served by access.listScopes.
 */

import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";

import {
  isPresentableScope,
  isSensitiveScope,
  scopeClosure,
  scopeLabel,
  type ScopeCatalogue,
} from "./scopeCatalogue";

export interface AddedPermission {
  scope: string;
  label: string;
  description: string;
  /** Scopes in the closure that are sensitive, the permission itself first. */
  sensitiveVia: string[];
  /** Labels of the other permissions a grant of this one carries along. */
  includes: string[];
  /** Null means everywhere; otherwise how many places the grant names. */
  places: number | null;
}

/**
 * The permissions a set of added grants gives, one per scope, with what each
 * carries along and whether any of it is sensitive. Exception and
 * provisioned scopes are not permissions a person reviews and are skipped.
 */
export function addedPermissions(
  addGrants: readonly SdkRoleGrant[],
  catalogue: ScopeCatalogue,
): AddedPermission[] {
  const byScope = new Map<string, AddedPermission>();
  for (const grant of addGrants) {
    const scope = grant.scope as string;
    if (!isPresentableScope(catalogue, scope)) continue;
    const places = grant.selectors ? grant.selectors.length : null;
    const existing = byScope.get(scope);
    if (existing) {
      existing.places =
        existing.places === null || places === null
          ? null
          : existing.places + places;
      continue;
    }
    const closure = scopeClosure(catalogue, scope);
    const entry = catalogue.get(scope);
    byScope.set(scope, {
      scope,
      label: scopeLabel(catalogue, scope),
      description: entry?.description ?? "",
      sensitiveVia: closure.filter((s) => isSensitiveScope(catalogue, s)),
      includes: closure
        .filter((s) => s !== scope && isPresentableScope(catalogue, s))
        .map((s) => scopeLabel(catalogue, s)),
      places,
    });
  }
  return [...byScope.values()];
}

/** The permissions a set of roles gives, in catalogue order, closed over inclusion. */
export function permissionsOfRoles(
  roles: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): string[] {
  const held = new Set<string>();
  for (const role of roles) {
    for (const grant of role.grants) {
      for (const scope of scopeClosure(catalogue, grant.scope as string)) {
        if (isPresentableScope(catalogue, scope)) held.add(scope);
      }
    }
  }
  const ordered = [...catalogue.keys()].filter((scope) => held.has(scope));
  // Scopes the catalogue has not loaded yet keep their slug at the end.
  for (const scope of held) {
    if (!catalogue.has(scope)) ordered.push(scope);
  }
  return ordered;
}

export interface PermissionSummary {
  /** Every permission, in catalogue order. */
  scopes: string[];
  /** The sensitive ones among them, in the same order. */
  sensitive: string[];
}

/** What one role gives, for a preview before it is assigned. */
export function rolePermissionSummary(
  role: Pick<Role, "grants">,
  catalogue: ScopeCatalogue,
): PermissionSummary {
  const scopes = permissionsOfRoles([role], catalogue);
  return {
    scopes,
    sensitive: scopes.filter((scope) => isSensitiveScope(catalogue, scope)),
  };
}

/**
 * What a member gains when their roles change from before to after: the
 * permissions the new set gives that the old set did not.
 */
export function permissionsAddedByRoles(
  before: readonly Pick<Role, "grants">[],
  after: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): PermissionSummary {
  const had = new Set(permissionsOfRoles(before, catalogue));
  const scopes = permissionsOfRoles(after, catalogue).filter(
    (scope) => !had.has(scope),
  );
  return {
    scopes,
    sensitive: scopes.filter((scope) => isSensitiveScope(catalogue, scope)),
  };
}
