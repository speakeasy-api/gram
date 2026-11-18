import type { QueryClient } from "@tanstack/react-query";

import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type {
  AgentPolicySelector,
  AgentPolicySelectorResourceKind,
} from "@gram/client/models/components/agentpolicyselector.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { isProjectWideSelector } from "@/pages/access/computePanelState";
import { isKnownResourceType, type ResourceType } from "@/pages/access/types";

import { ANY_RESOURCE, delegableGrantKey } from "./agent-api-key-grants";
import { CONNECT_SCOPE } from "./agentPolicyEdit";

/**
 * A permission per scope, in the shape the role editor's resource picker
 * already speaks: `null` is unrestricted, an array is one selector per chosen
 * resource, and `[]` is a choice the user has started but not finished.
 */
export type AgentPolicyDraft = Record<string, Selector[] | null>;

type EditableAgentPolicyGrant = AgentPolicyGrantForm & { effect: "allow" };

/**
 * Whether this permission offers a resource choice.
 *
 * Environments are not a list you pick from, matching the role editor. Risk
 * policy selectors are identified by a risk policy id while the shared picker
 * emits MCP server ids, so offering it would build a selector naming the wrong
 * kind of resource.
 */
export function isAgentPolicyNarrowable(resourceType: ResourceType): boolean {
  return (
    resourceType === "mcp" ||
    resourceType === "project" ||
    resourceType === "skill" ||
    resourceType === "inference" ||
    resourceType === "assistant"
  );
}

/**
 * What a permission on every resource of its kind applies to, as the editor and
 * the agent's profile show it. An org grant narrowed to a project is not whole,
 * so a caller passes no kind unless `isAgentPolicySelectorUnrestricted` holds.
 */
export function agentPolicyUnrestrictedLabel(kind: string | undefined): string {
  return kind === "org" ? "Whole organization" : "All resources";
}

/**
 * The resource kind a scope's selectors must carry. Mirrors
 * `ResourceKindForScope` in server/internal/authz/selector.go, which derives it
 * from the scope; `ValidateSelector` rejects a selector that disagrees.
 * mcp:auth grants are deliberately unrepresentable here until the auth client
 * editor exists, so stored ones are preserved and locked.
 */
const RESOURCE_KIND_BY_SCOPE_FAMILY: Record<
  string,
  AgentPolicySelectorResourceKind
> = {
  org: "org",
  project: "project",
  mcp: "mcp",
  environment: "environment",
  skill: "skill",
  assistant: "assistant",
  inference: "inference",
  risk_policy: "risk_policy",
};

export function agentPolicyResourceKind(
  scope: string,
): AgentPolicySelectorResourceKind {
  return RESOURCE_KIND_BY_SCOPE_FAMILY[scope.split(":")[0] ?? ""] ?? "*";
}

/**
 * The extra selector keys each resource kind accepts. Mirrors
 * `allowedSelectorKeys` in server/internal/authz/selector.go; any other key is
 * a 400, so the conversion drops rather than forwards them.
 */
const ALLOWED_SELECTOR_KEYS: Record<
  string,
  ("projectId" | "disposition" | "tool" | "serverUrl" | "serverIdentity")[]
> = {
  mcp: ["projectId", "disposition", "tool"],
  environment: ["projectId"],
  assistant: ["projectId"],
  inference: ["projectId"],
  risk_policy: ["serverUrl", "serverIdentity"],
};

function policySelector(
  scope: string,
  source: Partial<Selector> | undefined,
): AgentPolicySelector {
  const resourceKind = agentPolicyResourceKind(scope);
  const selector: AgentPolicySelector = {
    resourceKind,
    resourceId: source?.resourceId ?? ANY_RESOURCE,
  };
  for (const key of ALLOWED_SELECTOR_KEYS[resourceKind] ?? []) {
    const value = (source as Record<string, string | undefined> | undefined)?.[
      key
    ];
    // An absent dimension and a wildcard mean the same thing to the matcher,
    // so the narrower wire shape is the one that omits it.
    if (value !== undefined && value !== ANY_RESOURCE) {
      (selector as Record<string, string>)[key] = value;
    }
  }
  return selector;
}

/**
 * The grants to send for a draft: one per chosen resource, with the resource
 * kind forced from the scope and unsupported dimensions dropped. Duplicates
 * are collapsed because the server rejects a request that repeats a grant.
 */
export function agentPolicyGrantsFromDraft(
  draft: AgentPolicyDraft,
): EditableAgentPolicyGrant[] {
  const forms: EditableAgentPolicyGrant[] = [];
  const seen = new Set<string>();
  for (const [scope, selectors] of Object.entries(draft)) {
    const sources = selectors === null ? [undefined] : selectors;
    for (const source of sources) {
      const form: EditableAgentPolicyGrant = {
        effect: "allow",
        scope,
        selector: policySelector(scope, source),
      };
      const key = delegableGrantKey(form);
      if (seen.has(key)) continue;
      seen.add(key);
      forms.push(form);
    }
  }
  return forms;
}

/**
 * The dimensions the editor's draft can hold and reproduce exactly. Narrower
 * than `ALLOWED_SELECTOR_KEYS`: the server also accepts `server_url` and
 * `server_identity`, which have no control and nowhere in the draft selector.
 * It also accepts `project_id` on org:hooks_ingest alone, and org has no draft
 * dimension, so a project-narrowed hooks grant is preserved and locked. Tools
 * and dispositions are chosen only for mcp:connect, which the editor leaves
 * to the agent's setup flow, so no MCP scope it shows can display them.
 */
const DRAFT_DIMENSIONS: Record<string, readonly string[]> = {
  mcp: ["projectId"],
  environment: ["projectId"],
  assistant: ["projectId"],
  inference: ["projectId"],
};

/**
 * Whether a selector value constrains its dimension. Presence, not truthiness:
 * an empty value still does. A wildcard constrains nothing, so it reads the
 * same as an absent key — which is how `Selector.Matches` in
 * server/internal/authz/selector.go treats it.
 */
export function isAgentPolicyConstraint(value: unknown): value is string {
  return typeof value === "string" && value !== ANY_RESOURCE;
}

/** The dimensions a selector actually constrains. */
function constrainedDimensions(
  selector: AgentPolicySelector,
): Record<string, string> {
  const constrained: Record<string, string> = {};
  for (const [key, value] of Object.entries(selector)) {
    if (key === "resourceKind" || key === "resourceId") continue;
    if (!isAgentPolicyConstraint(value)) continue;
    constrained[key] = value;
  }
  return constrained;
}

/** Whether a selector reaches every resource of its kind, with nothing narrowing it. */
export function isAgentPolicySelectorUnrestricted(
  selector: AgentPolicySelector,
): boolean {
  return (
    selector.resourceId === ANY_RESOURCE &&
    Object.keys(constrainedDimensions(selector)).length === 0
  );
}

/**
 * What a grant naming no single resource reaches, in words, or `undefined`
 * when it names one. An org grant narrowed by nothing else is the whole
 * organization whether it stores `*` or the organization's own id: an agent
 * belongs to one organization. The narrowing a grant adds, such as a project,
 * is the caller's to show beside it.
 */
export function agentPolicyBroadGrantLabel(
  selector: AgentPolicySelector,
): string | undefined {
  const unconstrained =
    Object.keys(constrainedDimensions(selector)).length === 0;
  if (selector.resourceKind === "org" && unconstrained)
    return agentPolicyUnrestrictedLabel("org");
  if (selector.resourceId !== ANY_RESOURCE) return undefined;
  return agentPolicyUnrestrictedLabel(undefined);
}

/**
 * Whether the editor can hold this grant and write it back unchanged.
 *
 * A grant constraining a dimension the draft cannot carry would come back out
 * broader than it went in, and the save path replaces rather than updates — so
 * an unrelated edit would silently drop the constraint.
 */
export function isAgentPolicyGrantRepresentable(
  grant: AgentPolicyGrant,
): boolean {
  if (grant.effect !== "allow") return false;
  if (grant.selector.resourceKind !== agentPolicyResourceKind(grant.scope)) {
    return false;
  }
  const carriable = new Set(
    DRAFT_DIMENSIONS[grant.selector.resourceKind] ?? [],
  );
  return Object.keys(constrainedDimensions(grant.selector)).every((key) =>
    carriable.has(key),
  );
}

/** The stored ceiling as a draft the editor can change. */
export function agentPolicyDraftFromGrants(
  grants: AgentPolicyGrant[],
): AgentPolicyDraft {
  const draft: AgentPolicyDraft = {};
  for (const grant of grants) {
    const { selector } = grant;
    if (isAgentPolicySelectorUnrestricted(selector)) {
      draft[grant.scope] = null;
      continue;
    }
    // A scope already read as unrestricted stays unrestricted: it covers every
    // narrower selector stored alongside it.
    if (draft[grant.scope] === null) continue;
    const existing = draft[grant.scope] ?? [];
    draft[grant.scope] = [
      ...existing,
      {
        resourceKind: selector.resourceKind,
        resourceId: selector.resourceId,
        // Presence, not truthiness: an empty `tool` or `project_id` still
        // constrains the grant, and dropping it here would widen it.
        ...(selector.projectId !== undefined
          ? { projectId: selector.projectId }
          : {}),
        ...(selector.disposition !== undefined
          ? { disposition: selector.disposition }
          : {}),
        ...(selector.tool !== undefined ? { tool: selector.tool } : {}),
      },
    ];
  }
  return draft;
}

/**
 * Whether the editor's row and resource picker can show this scope's grants
 * exactly as stored. The picker shows one choice at a time: every resource,
 * whole projects, or named resources. A scope it cannot show that way is
 * locked rather than shown as something it is not, because a save from a
 * misread scope rewrites it as what was shown.
 */
function isAgentPolicyScopeShowable(
  scope: string,
  grants: AgentPolicyGrant[],
): boolean {
  // A scope names its resource type before the colon. One whose type the
  // editor does not know has no row to show it in.
  const resourceType = scope.split(":")[0] ?? "";
  if (!isKnownResourceType(resourceType)) return false;
  // Every resource of its kind, which covers any narrower grant beside it.
  if (grants.some((grant) => isAgentPolicySelectorUnrestricted(grant.selector)))
    return true;
  if (!isAgentPolicyNarrowable(resourceType)) {
    // The row only ever says "All resources" or "Whole organization": an org
    // grant naming the one organization is the whole organization, and any
    // other narrowing has nowhere to show.
    return grants.every(
      (grant) =>
        grant.selector.resourceKind === "org" &&
        Object.keys(constrainedDimensions(grant.selector)).length === 0,
    );
  }
  // The picker's own rule, so a project-wide grant stored with no resource id
  // is read as the project it ticks.
  const projectWide = grants.filter((grant) =>
    isProjectWideSelector(grant.selector),
  );
  const named = grants.filter(
    (grant) =>
      grant.selector.resourceId !== "" &&
      grant.selector.resourceId !== ANY_RESOURCE,
  );
  // Neither, such as every resource in an empty project: the picker has no
  // tick for it.
  if (projectWide.length + named.length !== grants.length) return false;
  // Whole projects: the picker's project list, offered for MCP servers,
  // assistants and inference.
  if (named.length === 0) {
    return (
      resourceType === "mcp" ||
      resourceType === "assistant" ||
      resourceType === "inference"
    );
  }
  // Named resources only: inference has no list of resources to name.
  if (projectWide.length === 0) return resourceType !== "inference";
  // Both at once: the picker can only show one of the two.
  return false;
}

export interface AgentPolicyView {
  /** The permissions the editor may change. */
  draft: AgentPolicyDraft;
  /** The stored grants the draft was built from; the only ones it may replace. */
  editable: AgentPolicyGrant[];
  /** Stored grants kept exactly as they are, and the scopes they lock. */
  preserved: AgentPolicyGrant[];
  preservedScopes: string[];
}

/**
 * Credential restrictions share the policy but are listed and edited in their
 * own panel, so the permission editor neither shows nor rewrites them.
 */
export const AGENT_CREDENTIAL_BLOCK_SCOPE = "mcp:blocked_auth";

/**
 * The servers an agent reaches are chosen in its own setup flow, beside the
 * sign-in each server needs, and listed in a section of their own. The
 * permission editor neither shows nor rewrites them.
 */
export function isAgentPolicyEditorScope(scope: string): boolean {
  return scope !== AGENT_CREDENTIAL_BLOCK_SCOPE && scope !== CONNECT_SCOPE;
}

/**
 * Split the stored ceiling into what the editor may rewrite and what it must
 * leave alone. A scope with even one unrepresentable grant is locked whole:
 * editing half of one would let a save drop the constrained grant and re-add a
 * broader one under the same scope. Credential restrictions and MCP server
 * grants are left out of both; the fingerprint of the full ceiling still
 * covers them.
 */
export function agentPolicyViewFromGrants(
  stored: AgentPolicyGrant[],
): AgentPolicyView {
  const grants = stored.filter((grant) =>
    isAgentPolicyEditorScope(grant.scope),
  );
  const byScope = new Map<string, AgentPolicyGrant[]>();
  for (const grant of grants) {
    byScope.set(grant.scope, [...(byScope.get(grant.scope) ?? []), grant]);
  }
  const locked = new Set(
    [...byScope]
      .filter(
        ([scope, scopeGrants]) =>
          !scopeGrants.every(isAgentPolicyGrantRepresentable) ||
          !isAgentPolicyScopeShowable(scope, scopeGrants),
      )
      .map(([scope]) => scope),
  );
  const preserved = grants.filter((grant) => locked.has(grant.scope));
  const editable = grants.filter((grant) => !locked.has(grant.scope));
  return {
    draft: agentPolicyDraftFromGrants(editable),
    editable,
    preserved,
    preservedScopes: [...locked],
  };
}

/**
 * A stable identity for the whole stored ceiling. Covers every grant, preserved
 * ones included, by id and by contents: a grant rewritten in place makes an
 * open draft's base just as stale as one added or removed.
 */
export function agentPolicyFingerprint(grants: AgentPolicyGrant[]): string {
  return JSON.stringify(
    grants
      .map((grant) =>
        JSON.stringify([
          grant.id,
          delegableGrantKey({
            effect: grant.effect,
            scope: grant.scope,
            selector: grant.selector,
          }),
        ]),
      )
      .sort(),
  );
}

/**
 * The scopes a draft changes from where it started, by the grants each would
 * write. A picker opened and closed, or a choice made and then undone, leaves
 * its scope out, so a save writes nothing for it.
 */
export function agentPolicyChangedScopes(
  initial: AgentPolicyDraft,
  draft: AgentPolicyDraft,
): Set<string> {
  // Only called once the scope is known to be in both drafts.
  const keys = (source: AgentPolicyDraft, scope: string) =>
    agentPolicyGrantsFromDraft({ [scope]: source[scope] ?? null })
      .map(delegableGrantKey)
      .sort();
  const changed = new Set<string>();
  for (const scope of new Set([
    ...Object.keys(initial),
    ...Object.keys(draft),
  ])) {
    if (scope in initial !== scope in draft) {
      changed.add(scope);
      continue;
    }
    if (
      JSON.stringify(keys(initial, scope)) !==
      JSON.stringify(keys(draft, scope))
    )
      changed.add(scope);
  }
  return changed;
}

export interface AgentPolicyDiff {
  create: EditableAgentPolicyGrant[];
  remove: AgentPolicyGrant[];
}

/**
 * What to send to reach `next` from the stored ceiling. Grant identity is the
 * effect, scope, and selector together, so a changed permission is a removal
 * and an addition rather than an update.
 */
export function diffAgentPolicyGrants(
  stored: AgentPolicyGrant[],
  next: EditableAgentPolicyGrant[],
): AgentPolicyDiff {
  const storedKey = (grant: AgentPolicyGrant) =>
    delegableGrantKey({
      effect: grant.effect,
      scope: grant.scope,
      selector: grant.selector,
    });
  const storedKeys = new Set(stored.map(storedKey));
  const nextKeys = new Set(next.map(delegableGrantKey));
  return {
    create: next.filter((form) => !storedKeys.has(delegableGrantKey(form))),
    // Every stored copy goes, so a duplicate left behind cannot keep the
    // permission a save removed.
    remove: stored.filter((grant) => !nextKeys.has(storedKey(grant))),
  };
}

/**
 * What a save sends: the grants of every scope the draft changed, and every
 * stored grant of a kept scope the reader chose to remove. A kept scope is
 * removed whole, because the editor cannot show it to narrow it.
 */
export function agentPolicySaveChanges(
  view: AgentPolicyView,
  draft: AgentPolicyDraft,
  removedScopes: ReadonlySet<string>,
): AgentPolicyDiff {
  const changedScopes = agentPolicyChangedScopes(view.draft, draft);
  const changes = diffAgentPolicyGrants(
    view.editable.filter((grant) => changedScopes.has(grant.scope)),
    agentPolicyGrantsFromDraft(draft).filter((grant) =>
      changedScopes.has(grant.scope),
    ),
  );
  return {
    create: changes.create,
    remove: [
      ...changes.remove,
      ...view.preserved.filter((grant) => removedScopes.has(grant.scope)),
    ],
  };
}

/**
 * Invalidate what a committed ceiling change makes stale. The delegable
 * candidate set matters most: it is what the API key dialog narrows, and it is
 * keyed by the authorizing user because it intersects *their* permissions.
 */
export function invalidateAgentPolicy(
  queryClient: QueryClient,
  organizationId: string,
  userId: string,
  agentID: string,
): Promise<unknown> {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: ["managed-agents", organizationId],
    }),
    queryClient.invalidateQueries({
      queryKey: ["agent-policy-grants", organizationId, agentID],
    }),
    queryClient.invalidateQueries({
      queryKey: ["agent-delegable-grants", organizationId, userId, agentID],
    }),
  ]);
}
