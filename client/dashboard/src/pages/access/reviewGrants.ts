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
  /** True when the permission, or something it includes, is sensitive. */
  sensitive: boolean;
  /**
   * Labels of the included permissions that make it sensitive, when it is
   * not sensitive itself: "Sensitive because it includes View tool logs".
   */
  sensitiveVia: string[];
  /** Labels of the other permissions a grant of this one carries along. */
  includes: string[];
  /** Null means everywhere; otherwise how many places the grant names. */
  places: number | null;
}

/**
 * The permissions a set of grants gives, one per scope, with what each
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
    const sensitiveItself = isSensitiveScope(catalogue, scope);
    const sensitiveVia = sensitiveItself
      ? []
      : closure
          .filter((s) => s !== scope && isSensitiveScope(catalogue, s))
          .map((s) => scopeLabel(catalogue, s));
    byScope.set(scope, {
      scope,
      label: scopeLabel(catalogue, scope),
      description: entry?.description ?? "",
      sensitive: sensitiveItself || sensitiveVia.length > 0,
      sensitiveVia,
      includes: closure
        .filter((s) => s !== scope && isPresentableScope(catalogue, s))
        .map((s) => scopeLabel(catalogue, s)),
      places,
    });
  }
  return [...byScope.values()];
}

/** The members and agents a save adds to a role, and what they gain by it. */
export interface NewHolders {
  members: number;
  agents: number;
  /** The sensitive permissions the role gives, which every new holder gains. */
  sensitive: AddedPermission[];
}

/** Whether a review needs the acknowledgement: something sensitive is granted. */
export function reviewNeedsConfirmation(
  added: AddedPermission[],
  newHolders: NewHolders | null,
): boolean {
  return (
    added.some((p) => p.sensitive) || (newHolders?.sensitive.length ?? 0) > 0
  );
}

/**
 * Exceptions a save lifts, as grants of the permission they withheld. Removing
 * a `*:blocked_*` grant from a role that still allows the scope gives the
 * scope back wherever the exception applied, so it is reviewed as an
 * addition. Exceptions on scopes the role no longer allows lift nothing.
 */
export function liftedExceptions(
  removeGrants: readonly SdkRoleGrant[],
  after: readonly SdkRoleGrant[],
  catalogue: ScopeCatalogue,
): SdkRoleGrant[] {
  const allowed = new Set(after.map((grant) => grant.scope as string));
  const lifted: SdkRoleGrant[] = [];
  for (const grant of removeGrants) {
    const base = baseScopeOfException(catalogue, grant.scope as string);
    if (base && allowed.has(base)) {
      lifted.push({ ...grant, scope: base as SdkRoleGrant["scope"] });
    }
  }
  return lifted;
}

function baseScopeOfException(
  catalogue: ScopeCatalogue,
  exception: string,
): string | undefined {
  for (const entry of catalogue.values()) {
    if (entry.exclusionScope === exception) return entry.slug as string;
  }
  return undefined;
}

/**
 * Whether a grant reaches every resource of its kind. The server stores an
 * unrestricted grant as a wildcard selector, so both forms count; a selector
 * that names a project, tool or server is narrower.
 */
function coversEverywhere(selectors: SdkRoleGrant["selectors"]): boolean {
  if (!selectors || selectors.length === 0) return true;
  return selectors.some(
    (s) =>
      s.resourceId === "*" &&
      !s.projectId &&
      !s.tool &&
      !s.disposition &&
      !s.serverUrl,
  );
}

/**
 * How widely a set of roles holds each permission, closed over inclusion:
 * true when some grant reaches every resource, false when every grant names
 * some. A grant of mcp:write on one server holds mcp:read on that server.
 */
function coverageOfRoles(
  roles: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): Map<string, boolean> {
  const held = new Map<string, boolean>();
  for (const role of roles) {
    for (const grant of role.grants) {
      const everywhere = coversEverywhere(grant.selectors);
      for (const scope of scopeClosure(catalogue, grant.scope as string)) {
        if (!isPresentableScope(catalogue, scope)) continue;
        held.set(scope, (held.get(scope) ?? false) || everywhere);
      }
    }
  }
  return held;
}

/** Scopes in catalogue order; ones the catalogue has not loaded keep their slug at the end. */
function inCatalogueOrder(
  scopes: Iterable<string>,
  catalogue: ScopeCatalogue,
): string[] {
  const wanted = new Set(scopes);
  const ordered = [...catalogue.keys()].filter((scope) => wanted.has(scope));
  for (const scope of wanted) {
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

/**
 * What a member gains when their roles change from before to after: the
 * permissions the new set gives that the old set did not, and the ones the
 * old set gave only on some resources that the new set gives everywhere. A
 * member who could read one project's tool logs and is given unrestricted
 * log:read gains log:read; one who already held it everywhere gains nothing
 * from a narrower grant. The server's audit row makes the same comparison.
 */
export function permissionsAddedByRoles(
  before: readonly Pick<Role, "grants">[],
  after: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): PermissionSummary {
  const had = coverageOfRoles(before, catalogue);
  const has = coverageOfRoles(after, catalogue);
  const added: string[] = [];
  for (const [scope, everywhere] of has) {
    const held = had.get(scope);
    if (held === undefined || (everywhere && !held)) added.push(scope);
  }
  const scopes = inCatalogueOrder(added, catalogue);
  return {
    scopes,
    sensitive: scopes.filter((scope) => isSensitiveScope(catalogue, scope)),
  };
}
