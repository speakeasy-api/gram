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

type Selector = NonNullable<SdkRoleGrant["selectors"]>[number];

/**
 * Whether a selector reaches every resource of its kind. The server stores an
 * unrestricted grant as a wildcard selector, so it counts; a selector that
 * names a resource, project, tool or server is narrower.
 */
function coversEveryResource(selector: Selector): boolean {
  return Object.entries(selector).every(
    ([key, value]) =>
      key === "resourceKind" ||
      (key === "resourceId" ? value === "*" : value === undefined),
  );
}

/** One key per resource a selector names, however its fields were set. */
function resourceKey(selector: Selector): string {
  return JSON.stringify(
    Object.entries(selector)
      .filter(([, value]) => value !== undefined)
      .sort(([a], [b]) => a.localeCompare(b)),
  );
}

/**
 * How widely a set of roles holds one permission: everywhere, or on the
 * resources its grants name.
 */
interface Coverage {
  everywhere: boolean;
  /** Keys of the resources named by grants that reach some, not every. */
  resources: Set<string>;
}

function extendCoverage(
  held: Map<string, Coverage>,
  scope: string,
  selectors: SdkRoleGrant["selectors"],
): void {
  const reach = held.get(scope) ?? { everywhere: false, resources: new Set() };
  if (!selectors || selectors.length === 0) reach.everywhere = true;
  for (const selector of selectors ?? []) {
    if (coversEveryResource(selector)) reach.everywhere = true;
    else reach.resources.add(resourceKey(selector));
  }
  held.set(scope, reach);
}

/** Whether everything `after` reaches, `before` reached too. */
function covers(before: Coverage, after: Coverage): boolean {
  if (before.everywhere) return true;
  if (after.everywhere) return false;
  return [...after.resources].every((key) => before.resources.has(key));
}

/**
 * How widely a set of roles holds each permission, closed over inclusion. A
 * grant of mcp:write on one server holds mcp:read on that server.
 */
function coverageOfRoles(
  roles: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): Map<string, Coverage> {
  const held = new Map<string, Coverage>();
  for (const role of roles) {
    for (const grant of role.grants) {
      for (const scope of scopeClosure(catalogue, grant.scope as string)) {
        if (!isPresentableScope(catalogue, scope)) continue;
        extendCoverage(held, scope, grant.selectors);
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
 * permissions the new set reaches somewhere the old set did not. A member
 * who could read one project's tool logs and is given log:read on a second
 * project, or everywhere, gains log:read; one who already held it everywhere
 * gains nothing from a narrower grant. The server's audit row makes the same
 * comparison.
 */
export function permissionsAddedByRoles(
  before: readonly Pick<Role, "grants">[],
  after: readonly Pick<Role, "grants">[],
  catalogue: ScopeCatalogue,
): PermissionSummary {
  const had = coverageOfRoles(before, catalogue);
  const has = coverageOfRoles(after, catalogue);
  const added: string[] = [];
  for (const [scope, reach] of has) {
    const held = had.get(scope);
    if (held === undefined || !covers(held, reach)) added.push(scope);
  }
  const scopes = inCatalogueOrder(added, catalogue);
  return {
    scopes,
    sensitive: scopes.filter((scope) => isSensitiveScope(catalogue, scope)),
  };
}
