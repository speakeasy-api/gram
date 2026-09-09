import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type {
  AgentPolicySelector,
  AgentPolicySelectorDisposition,
} from "@gram/client/models/components/agentpolicyselector.js";

/** A selector value meaning "every resource of this kind". */
export const ANY_RESOURCE = "*";

/**
 * The dimensions a delegated request may pin on top of a policy grant.
 * Mirrors allowedSelectorKeys in server/internal/authz/selector.go — the
 * server rejects any other key, so the editor never offers one.
 */
const NARROWABLE_BY_KIND: Record<string, NarrowingDimension[]> = {
  mcp: ["projectId", "disposition", "tool"],
  environment: ["projectId"],
};

export type NarrowingDimension = "projectId" | "disposition" | "tool";

/** Which inventory names the resources a selector of this kind identifies. */
export type ResourceInventory = "mcp" | "project";

export interface GrantNarrowing {
  resourceId?: string;
  projectId?: string;
  disposition?: AgentPolicySelectorDisposition;
  tool?: string;
}

export interface GrantSelection {
  grant: AgentPolicyGrantForm;
  narrowing: GrantNarrowing;
}

export const DISPOSITION_LABELS: Record<
  AgentPolicySelectorDisposition,
  string
> = {
  read_only: "Read-only tools",
  destructive: "Destructive tools",
  idempotent: "Idempotent tools",
  open_world: "Open-world tools",
};

export function resourceInventoryFor(
  resourceKind: string,
): ResourceInventory | null {
  switch (resourceKind) {
    case "mcp":
      return "mcp";
    // Project and skill grants are both identified by a project id.
    case "project":
    case "skill":
      return "project";
    default:
      return null;
  }
}

/**
 * The dimensions this grant leaves open. A dimension the agent policy already
 * pins stays fixed: replacing it would request authority the policy never
 * granted, and the server would reject it.
 */
export function openDimensions(
  grant: AgentPolicyGrantForm,
): NarrowingDimension[] {
  const available = NARROWABLE_BY_KIND[grant.selector.resourceKind] ?? [];
  return available.filter((key) => grant.selector[key] === undefined);
}

/** Whether the caller may pick one resource in place of the policy wildcard. */
export function canNarrowResource(grant: AgentPolicyGrantForm): boolean {
  return (
    grant.selector.resourceId === ANY_RESOURCE &&
    resourceInventoryFor(grant.selector.resourceKind) !== null
  );
}

/**
 * Mirrors Selector.StrictMatches in server/internal/authz/selector.go: for the
 * live policy grant to cover a request, every dimension the policy constrains
 * must appear in the request with the same value, unless the policy wildcards
 * it. Anything else is a broader request than the policy allows.
 */
export function requestNarrowsPolicy(
  policy: AgentPolicySelector,
  requested: AgentPolicySelector,
): boolean {
  const asked = requested as Record<string, string | undefined>;
  return Object.entries(policy as Record<string, string | undefined>).every(
    ([key, value]) => {
      if (value === undefined) return true;
      const chosen = asked[key];
      if (chosen === undefined) return false;
      return value === ANY_RESOURCE || value === chosen;
    },
  );
}

function buildRequestedGrant(selection: GrantSelection): AgentPolicyGrantForm {
  const { grant, narrowing } = selection;
  const selector: AgentPolicySelector = { ...grant.selector };
  if (narrowing.resourceId && canNarrowResource(grant)) {
    selector.resourceId = narrowing.resourceId;
  }
  const open = new Set(openDimensions(grant));
  if (open.has("projectId") && narrowing.projectId) {
    selector.projectId = narrowing.projectId;
  }
  if (open.has("disposition") && narrowing.disposition) {
    selector.disposition = narrowing.disposition;
  }
  if (open.has("tool") && narrowing.tool) {
    selector.tool = narrowing.tool;
  }
  return { effect: "allow", scope: grant.scope, selector };
}

/**
 * Stable identity of a grant. Delegable candidates carry no id, so this keys
 * both the editor's per-candidate state and the duplicate check the server
 * would otherwise fail on.
 */
export function delegableGrantKey(form: AgentPolicyGrantForm): string {
  const selector = form.selector as Record<string, string | undefined>;
  const pairs = Object.keys(selector)
    .filter((key) => selector[key] !== undefined)
    .sort()
    .map((key) => `${key}=${selector[key]}`);
  return `${form.scope}|${pairs.join("&")}`;
}

/**
 * The exact grants to request for the selected policy grants. Throws rather
 * than sending anything the agent policy does not already cover: the server is
 * the authority, and a request it would reject is an editor bug, not a prompt.
 */
export function buildRequestedGrants(
  selections: GrantSelection[],
): AgentPolicyGrantForm[] {
  const forms: AgentPolicyGrantForm[] = [];
  const seen = new Set<string>();
  for (const selection of selections) {
    const form = buildRequestedGrant(selection);
    if (!requestNarrowsPolicy(selection.grant.selector, form.selector)) {
      throw new Error(
        "A requested permission is broader than the agent policy allows. Reset it and try again.",
      );
    }
    const identity = delegableGrantKey(form);
    if (seen.has(identity)) {
      throw new Error(
        "Two requested permissions are identical. Narrow or remove one of them.",
      );
    }
    seen.add(identity);
    forms.push(form);
  }
  return forms;
}

// Match Go strings.TrimSpace and utf8.RuneCountInString used by agent issuance.
export function validateAgentAPIKeyName(value: string): string {
  const name = value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
  if (!name) throw new Error("Enter a key name.");
  for (const prefix of ["plugins-", "litellm-"]) {
    if (name.startsWith(prefix))
      throw new Error(
        `Key names starting with "${prefix}" are reserved. Choose another name.`,
      );
  }
  if (Array.from(name).length > 255)
    throw new Error(
      "Key names must not exceed 255 Unicode characters. Shorten the name.",
    );
  return name;
}
