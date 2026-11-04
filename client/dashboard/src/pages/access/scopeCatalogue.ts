/**
 * Pure helpers over the scope catalogue served by access.listScopes. Every
 * label, description, inclusion and sensitivity the dashboard shows comes
 * from here, so the server stays the only place that describes a scope.
 */

import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { ScopeTask } from "@gram/client/models/components/scopetask.js";

export type ScopeCatalogue = ReadonlyMap<string, ScopeDefinition>;

const EMPTY_SCOPE_CATALOGUE: ScopeCatalogue = new Map();

export function scopeCatalogue(
  scopes: readonly ScopeDefinition[] | undefined,
): ScopeCatalogue {
  if (!scopes) return EMPTY_SCOPE_CATALOGUE;
  return new Map(scopes.map((scope) => [scope.slug, scope]));
}

/** The catalogue label for a scope, or the slug while the catalogue loads. */
export function scopeLabel(catalogue: ScopeCatalogue, slug: string): string {
  return catalogue.get(slug)?.label ?? slug;
}

/** "View tool logs (log:read)" once the catalogue is loaded, else the slug. */
export function scopeTitle(catalogue: ScopeCatalogue, slug: string): string {
  const label = catalogue.get(slug)?.label;
  return label ? `${label} (${slug})` : slug;
}

/**
 * True when a grant of `grantScope` satisfies a check for `scope`, either
 * directly or because the server says the grant includes it (mcp:write
 * includes mcp:connect).
 */
export function scopeCovers(
  catalogue: ScopeCatalogue,
  grantScope: string,
  scope: string,
): boolean {
  if (grantScope === scope) return true;
  return catalogue.get(grantScope)?.subScopes.includes(scope) ?? false;
}

/** The scope and everything a grant of it carries along. */
export function scopeClosure(
  catalogue: ScopeCatalogue,
  scope: string,
): string[] {
  return [scope, ...(catalogue.get(scope)?.subScopes ?? [])];
}

/** Exception scopes and provisioned auth-client scopes are not permissions a
 * person reads in a role; they never count as "more than this". */
export function isPresentableScope(
  catalogue: ScopeCatalogue,
  slug: string,
): boolean {
  const entry = catalogue.get(slug);
  if (!entry) return !slug.includes(":blocked_");
  return (
    entry.visibility === "user_visible" && entry.category !== "mcp_auth_client"
  );
}

export function isSensitiveScope(
  catalogue: ScopeCatalogue,
  slug: string,
): boolean {
  return catalogue.get(slug)?.sensitive ?? false;
}

/**
 * True when the task needs the scope: it lists the scope itself or one
 * whose grant includes it, so a denied mcp:read is part of a task that lists
 * mcp:write.
 */
export function taskNeeds(
  catalogue: ScopeCatalogue,
  task: ScopeTask,
  scope: string,
): boolean {
  return task.scopes.some((listed) => scopeCovers(catalogue, listed, scope));
}

/** Tasks that need the scope, so a denial can say what task it is part of. */
export function tasksNeeding(
  catalogue: ScopeCatalogue,
  tasks: readonly ScopeTask[],
  scope: string,
): ScopeTask[] {
  return tasks.filter((task) => taskNeeds(catalogue, task, scope));
}
