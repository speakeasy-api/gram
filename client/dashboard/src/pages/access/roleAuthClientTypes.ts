import { CLIENT_TYPES } from "@/lib/auth-clients";
import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";

/**
 * A role's rule for one kind of upstream credential. Only rules that cover a
 * whole credential type, in every project, are edited here; rules naming one
 * credential, or confined to a project, are left exactly as they are.
 */
export type ClientTypeAccess = "allow" | "block" | "unset";

export type ClientTypeRules = Record<string, ClientTypeAccess>;

const REPRESENTED_USER = "$represented_user";

type StoredRule = {
  scope: "mcp:auth" | "mcp:blocked_auth";
  selector: Selector;
};

function isTypeRule(selector: Selector): boolean {
  return (
    selector.resourceKind === "mcp_auth_client" &&
    selector.resourceId === "*" &&
    !!selector.clientType &&
    selector.clientType !== "*" &&
    selector.projectId == null &&
    selector.tool == null &&
    selector.disposition == null &&
    selector.serverUrl == null &&
    selector.upstreamClient == null &&
    (selector.connectionOwner == null ||
      selector.connectionOwner === REPRESENTED_USER)
  );
}

function storedTypeRules(role: Role | undefined): StoredRule[] {
  const rules: StoredRule[] = [];
  for (const grant of role?.grants ?? []) {
    const scope = grant.scope as string;
    if (scope !== "mcp:auth" && scope !== "mcp:blocked_auth") {
      continue;
    }
    for (const selector of (grant.selectors ?? []) as Selector[]) {
      if (isTypeRule(selector)) {
        rules.push({ scope, selector });
      }
    }
  }
  return rules;
}

/** The role's current rule per credential type. A block outranks an allow. */
export function clientTypeRulesFromRole(
  role: Role | undefined,
): ClientTypeRules {
  const result: ClientTypeRules = Object.fromEntries(
    CLIENT_TYPES.map((type) => [type, "unset" as ClientTypeAccess]),
  );
  for (const rule of storedTypeRules(role)) {
    const type = rule.selector.clientType!;
    if (rule.scope === "mcp:blocked_auth") {
      result[type] = "block";
    } else if (result[type] !== "block") {
      result[type] = "allow";
    }
  }
  return result;
}

/**
 * The selector an allow or block for one type is written with. A personal
 * credential is allowed only for the person's own connection, so the stored
 * rule says what the engine enforces.
 */
function typeSelector(type: string, access: "allow" | "block"): Selector {
  const selector: Selector = {
    resourceKind: "mcp_auth_client",
    resourceId: "*",
    clientType: type as Selector["clientType"],
  };
  if (access === "allow" && type === "remote_session") {
    selector.connectionOwner = REPRESENTED_USER;
  }
  return selector;
}

function group(
  rules: { scope: StoredRule["scope"]; selector: Selector }[],
): SdkRoleGrant[] {
  const byScope = new Map<string, Selector[]>();
  for (const rule of rules) {
    byScope.set(rule.scope, [
      ...(byScope.get(rule.scope) ?? []),
      rule.selector,
    ]);
  }
  return [...byScope].map(([scope, selectors]) => ({
    scope: scope as SdkRoleGrant["scope"],
    selectors,
  }));
}

/**
 * The grants to add and remove so the role matches `next`. Removals use the
 * stored selectors, so a type whose rule did not change is never rewritten.
 */
export function clientTypeGrantChanges(
  role: Role | undefined,
  next: ClientTypeRules,
): { addGrants: SdkRoleGrant[]; removeGrants: SdkRoleGrant[] } {
  const before = clientTypeRulesFromRole(role);
  const stored = storedTypeRules(role);
  const add: { scope: StoredRule["scope"]; selector: Selector }[] = [];
  const remove: StoredRule[] = [];

  for (const type of CLIENT_TYPES) {
    const want = next[type] ?? "unset";
    if (want === before[type]) continue;
    remove.push(...stored.filter((rule) => rule.selector.clientType === type));
    if (want !== "unset") {
      add.push({
        scope: want === "allow" ? "mcp:auth" : "mcp:blocked_auth",
        selector: typeSelector(type, want),
      });
    }
  }

  return { addGrants: group(add), removeGrants: group(remove) };
}

export function clientTypeRulesKey(rules: ClientTypeRules): string {
  return CLIENT_TYPES.map((type) => `${type}=${rules[type] ?? "unset"}`).join(
    ",",
  );
}
