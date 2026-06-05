import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant as SdkRoleGrant } from "@gram/client/models/components/rolegrant.js";

import type {
  AnnotationHint,
  PolicyEffect,
  RoleGrant,
  ScopeRule,
  Selector,
} from "./types";
import { DISPOSITION_TO_ANNOTATION } from "./types";

/** Split a flat selector array into groups by hierarchy level. */
function groupSelectorsByLevel(selectors: Selector[]): Selector[][] {
  const projects: Selector[] = [];
  const servers: Selector[] = [];
  const tools: Selector[] = [];
  const annotations: Selector[] = [];

  for (const s of selectors) {
    if (s.disposition) annotations.push(s);
    else if (s.tool) tools.push(s);
    else if (s.projectId) projects.push(s);
    else servers.push(s);
  }

  const groups: Selector[][] = [];
  if (projects.length) groups.push(projects);
  if (servers.length) groups.push(servers);
  if (tools.length) groups.push(tools);
  if (annotations.length) groups.push(annotations);
  return groups;
}

/**
 * Detect the storage form of an unrestricted grant: a single selector with
 * only resource_kind + resource_id keys where resource_id is "*". The server
 * synthesizes this when a grant is created with nil selectors, so we collapse
 * it back to the unrestricted rule shape on read.
 */
function isUnrestrictedSelectorList(selectors: Selector[]): boolean {
  if (selectors.length !== 1) return false;
  const s = selectors[0]!;
  if (s.resourceId !== "*") return false;
  return (
    s.disposition == null &&
    s.tool == null &&
    s.projectId == null &&
    s.serverUrl == null
  );
}

/** Convert API Role grants to rules-based RoleGrant map. */
export function grantsFromRole(role: Role): Record<string, RoleGrant> {
  const result: Record<string, RoleGrant> = {};

  for (const g of role.grants) {
    if (!result[g.scope]) {
      result[g.scope] = { scope: g.scope, rules: [] };
    }

    const effect: PolicyEffect = (g.effect as PolicyEffect) ?? "allow";

    // Only collapse the synthetic wildcard for allow grants. sdkGrantsFromForm
    // re-emits unrestricted allow via selectors:undefined, but has no equivalent
    // for deny — collapsing a deny wildcard to selectors:null would drop the
    // deny on the next save. The backend intentionally keeps the explicit
    // kind-scoped wildcard for deny grants (see scopeWildcardStaysExplicit in
    // server/internal/authz/scopes_test.go).
    if (
      !g.selectors ||
      g.selectors.length === 0 ||
      (effect === "allow" &&
        isUnrestrictedSelectorList(g.selectors as Selector[]))
    ) {
      result[g.scope].rules.push({
        id: crypto.randomUUID(),
        effect,
        selectors: null,
      });
    } else {
      // Split selectors by hierarchy level into separate rules
      const groups = groupSelectorsByLevel(g.selectors as Selector[]);
      for (const sels of groups) {
        const rule: ScopeRule = {
          id: crypto.randomUUID(),
          effect,
          selectors: sels,
        };
        // Detect annotation-based rules and restore UI hints
        if (sels.some((s) => s.disposition)) {
          rule.customTab = "auto-groups";
          rule.annotations = sels
            .filter((s) => s.disposition)
            .map((s) => DISPOSITION_TO_ANNOTATION[s.disposition!])
            .filter((a): a is AnnotationHint => !!a);
        }
        result[g.scope].rules.push(rule);
      }
    }
  }

  return result;
}

export function sdkGrantsFromForm(
  grants: Record<string, RoleGrant>,
): SdkRoleGrant[] {
  const sdkGrants: SdkRoleGrant[] = [];

  for (const grant of Object.values(grants)) {
    const allowSelectors: Selector[] = [];
    const denySelectors: Selector[] = [];
    let hasUnrestrictedAllow = false;

    for (const rule of grant.rules) {
      if (rule.effect === "allow") {
        if (rule.selectors === null) hasUnrestrictedAllow = true;
        else if (rule.selectors.length > 0) {
          allowSelectors.push(...rule.selectors);
        }
      } else if (rule.selectors && rule.selectors.length > 0) {
        denySelectors.push(...rule.selectors);
      }
    }

    if (!hasUnrestrictedAllow && allowSelectors.length === 0) continue;

    sdkGrants.push({
      scope: grant.scope,
      selectors: hasUnrestrictedAllow ? undefined : allowSelectors,
    });

    if (denySelectors.length > 0) {
      sdkGrants.push({
        scope: grant.scope,
        effect: "deny",
        selectors: denySelectors,
      });
    }
  }

  return sdkGrants;
}

type GrantIdentity = {
  scope: SdkRoleGrant["scope"];
  effect: "allow" | "deny";
  selector?: Selector;
};

function selectorKey(selector: Selector | undefined): string {
  if (!selector) return "*";
  return JSON.stringify({
    disposition: selector.disposition ?? "",
    projectId: selector.projectId ?? "",
    resourceId: selector.resourceId,
    resourceKind: selector.resourceKind,
    serverUrl: selector.serverUrl ?? "",
    tool: selector.tool ?? "",
  });
}

function grantIdentityKey(identity: GrantIdentity): string {
  return `${identity.scope}\x00${identity.effect}\x00${selectorKey(identity.selector)}`;
}

function grantIdentities(grants: SdkRoleGrant[]): GrantIdentity[] {
  return grants.flatMap((grant) => {
    const effect = (grant.effect ?? "allow") as "allow" | "deny";
    if (!grant.selectors || grant.selectors.length === 0) {
      return [{ scope: grant.scope, effect }];
    }
    return grant.selectors.map((selector) => ({
      scope: grant.scope,
      effect,
      selector: selector as Selector,
    }));
  });
}

function grantsFromIdentities(identities: GrantIdentity[]): SdkRoleGrant[] {
  const unrestricted: SdkRoleGrant[] = [];
  const grouped = new Map<string, SdkRoleGrant>();

  for (const identity of identities) {
    if (!identity.selector) {
      unrestricted.push({
        scope: identity.scope,
        effect: identity.effect === "deny" ? "deny" : undefined,
        selectors: undefined,
      });
      continue;
    }

    const key = `${identity.scope}\x00${identity.effect}`;
    const existing = grouped.get(key);
    if (existing) {
      existing.selectors = [...(existing.selectors ?? []), identity.selector];
      continue;
    }

    grouped.set(key, {
      scope: identity.scope,
      effect: identity.effect === "deny" ? "deny" : undefined,
      selectors: [identity.selector],
    });
  }

  return [...unrestricted, ...grouped.values()];
}

export function diffGrants(
  before: SdkRoleGrant[],
  after: SdkRoleGrant[],
): { addGrants: SdkRoleGrant[]; removeGrants: SdkRoleGrant[] } {
  const beforeByKey = new Map(
    grantIdentities(before).map((identity) => [
      grantIdentityKey(identity),
      identity,
    ]),
  );
  const afterByKey = new Map(
    grantIdentities(after).map((identity) => [
      grantIdentityKey(identity),
      identity,
    ]),
  );

  const addIdentities = [...afterByKey]
    .filter(([key]) => !beforeByKey.has(key))
    .map(([, identity]) => identity);
  const removeIdentities = [...beforeByKey]
    .filter(([key]) => !afterByKey.has(key))
    .map(([, identity]) => identity);

  return {
    addGrants: grantsFromIdentities(addIdentities),
    removeGrants: grantsFromIdentities(removeIdentities),
  };
}

/**
 * Pure helper for the chip X-click behavior on a single scope's grant.
 * Returns the next grant value, or null when the scope should be removed
 * entirely from the form state.
 *
 *   - removing a narrower allow that leaves no allows → fall back to
 *     unrestricted ("All servers"); orphaned denies are dropped.
 *   - removing an unrestricted allow → orphan-clear: scope unchecks, any
 *     denies go with it.
 *   - removing a deny → just filter it out; allows preserved.
 *   - removing one of several allows → just filter it out.
 */
export function applyRemoveRule(
  grant: RoleGrant,
  ruleIndex: number,
): RoleGrant | null {
  const removed = grant.rules[ruleIndex];
  if (!removed) return grant;

  let rules = grant.rules.filter((_, i) => i !== ruleIndex);
  const hasAllow = rules.some((r) => r.effect === "allow");

  if (removed.effect === "allow" && removed.selectors !== null && !hasAllow) {
    rules = [{ id: crypto.randomUUID(), effect: "allow", selectors: null }];
  } else if (!hasAllow) {
    // No allows left — any remaining denies are orphaned, clear them too.
    rules = [];
  }

  if (rules.length === 0) return null;
  return { ...grant, rules };
}
