import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";

import {
  type CredentialRule,
  credentialRuleChanges,
  isCredentialScope,
} from "./credentialAccessModel";

/** Every upstream credential rule a role stores, one per selector. */
export function credentialRulesFromRole(
  role: Role | undefined,
): CredentialRule[] {
  const rules: CredentialRule[] = [];
  for (const grant of role?.grants ?? []) {
    const scope = grant.scope as string;
    if (!isCredentialScope(scope)) continue;
    for (const selector of (grant.selectors ?? []) as Selector[]) {
      rules.push({ scope, selector });
    }
  }
  return rules;
}

function group(rules: CredentialRule[]): SdkRoleGrant[] {
  const byScope = new Map<string, Selector[]>();
  for (const rule of rules) {
    byScope.set(rule.scope, [
      ...(byScope.get(rule.scope) ?? []),
      rule.selector as Selector,
    ]);
  }
  return [...byScope].map(([scope, selectors]) => ({
    scope: scope as SdkRoleGrant["scope"],
    selectors,
  }));
}

/**
 * The grants to add and remove so the role stores `next`. Removals use the
 * stored selectors, so a rule nobody changed is never rewritten.
 */
export function credentialGrantChanges(
  role: Role | undefined,
  next: CredentialRule[],
): { addGrants: SdkRoleGrant[]; removeGrants: SdkRoleGrant[] } {
  const { add, remove } = credentialRuleChanges(
    credentialRulesFromRole(role),
    next,
  );
  return { addGrants: group(add), removeGrants: group(remove) };
}
