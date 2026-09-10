import type { QueryClient } from "@tanstack/react-query";

import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type {
  AgentPolicySelector,
  AgentPolicySelectorResourceKind,
} from "@gram/client/models/components/agentpolicyselector.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { ScopeGroup } from "@/pages/access/RolePermissionsSection";
import type { ResourceType } from "@/pages/access/types";

import { ANY_RESOURCE, delegableGrantKey } from "./agent-api-key-grants";

/**
 * The scopes an agent policy ceiling may use.
 *
 * Mirrors the `safeRuntimeScope()` entries of `runtimeScopeDefinitions` in
 * server/internal/agents/runtimepolicy/scopes.go. Everything else in the
 * registry — org, chat, agent, every `blocked_` exclusion, risk policy bypass
 * and block — is rejected with 400, and `ValidateRuntimeScope` walks the whole
 * implication closure, so a scope only belongs here if everything it implies is
 * safe too.
 *
 * This is mirrored rather than filtered out of `access.listScopes` because that
 * endpoint requires `org:read`, and an owner configuring their own agent is
 * meant to need no RBAC grant at all. The server stays authoritative: a stale
 * mirror fails closed on a scope the server rejects, it cannot widen anything.
 */
export const AGENT_POLICY_SCOPES: ScopeDefinition[] = [
  {
    slug: "mcp:connect",
    resourceType: "mcp",
    visibility: "user_visible",
    description: "Connect to MCP servers and call their tools.",
  },
  {
    slug: "mcp:read",
    resourceType: "mcp",
    visibility: "user_visible",
    description: "View MCP servers and configuration.",
  },
  {
    slug: "mcp:write",
    resourceType: "mcp",
    visibility: "user_visible",
    description: "Create and modify MCP servers and configuration.",
  },
  {
    slug: "project:read",
    resourceType: "project",
    visibility: "user_visible",
    description: "View projects and project-related resources.",
  },
  {
    slug: "project:write",
    resourceType: "project",
    visibility: "user_visible",
    description: "Create and modify projects and project-related resources.",
  },
  {
    slug: "environment:read",
    resourceType: "environment",
    visibility: "user_visible",
    description: "View environments and their entries within the project.",
  },
  {
    slug: "environment:write",
    resourceType: "environment",
    visibility: "user_visible",
    description:
      "Add, edit, clone, and remove environments within the project.",
  },
  {
    slug: "skill:read",
    resourceType: "skill",
    visibility: "user_visible",
    description: "View skills within the project.",
  },
  {
    slug: "skill:write",
    resourceType: "skill",
    visibility: "user_visible",
    description: "Create and modify skills within the project.",
  },
  {
    slug: "risk_policy:evaluate",
    resourceType: "risk_policy",
    visibility: "user_visible",
    description: "Evaluate risk policies.",
  },
];

export const AGENT_POLICY_SCOPE_GROUPS: ScopeGroup[] = [
  {
    label: "MCP Servers",
    resourceType: "mcp",
    description: "MCP server configuration and connections.",
    scopes: AGENT_POLICY_SCOPES.filter((s) => s.resourceType === "mcp"),
  },
  {
    label: "Build & Deploy",
    resourceType: "project",
    description: "Projects and their related resources.",
    scopes: AGENT_POLICY_SCOPES.filter((s) => s.resourceType === "project"),
  },
  {
    label: "Environments",
    resourceType: "environment",
    description: "Environments and their entries within projects.",
    scopes: AGENT_POLICY_SCOPES.filter((s) => s.resourceType === "environment"),
  },
  {
    label: "Skills",
    resourceType: "skill",
    description: "Skills available within projects.",
    scopes: AGENT_POLICY_SCOPES.filter((s) => s.resourceType === "skill"),
  },
  {
    label: "Risk Policies",
    resourceType: "risk_policy",
    description: "Risk policy evaluation for MCP traffic.",
    scopes: AGENT_POLICY_SCOPES.filter((s) => s.resourceType === "risk_policy"),
  },
];

/**
 * A permission per scope, in the shape the role editor's resource picker
 * already speaks: `null` is unrestricted, an array is one selector per chosen
 * resource, and `[]` is a choice the user has started but not finished.
 */
export type AgentPolicyDraft = Record<string, Selector[] | null>;

/**
 * Whether this permission offers a resource choice.
 *
 * Environments are not a list you pick from, matching the role editor. Risk
 * policies are excluded for a different reason: their selectors are identified
 * by a risk policy id and narrowed by `server_url` / `server_identity`, and the
 * shared picker emits MCP server ids. Offering it would build a selector whose
 * resource id names the wrong kind of thing, so the permission applies to all
 * risk policies or is not added at all.
 */
export function isAgentPolicyNarrowable(resourceType: ResourceType): boolean {
  return (
    resourceType === "mcp" ||
    resourceType === "project" ||
    resourceType === "skill"
  );
}

/**
 * The resource kind a scope's selectors must carry.
 *
 * Mirrors `ResourceKindForScope` in server/internal/authz/selector.go, which
 * derives the kind from the scope rather than trusting the request:
 * `ValidateSelector` rejects any selector whose kind does not match.
 */
const RESOURCE_KIND_BY_SCOPE_FAMILY: Record<
  string,
  AgentPolicySelectorResourceKind
> = {
  project: "project",
  mcp: "mcp",
  environment: "environment",
  skill: "skill",
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
): AgentPolicyGrantForm[] {
  const forms: AgentPolicyGrantForm[] = [];
  const seen = new Set<string>();
  for (const [scope, selectors] of Object.entries(draft)) {
    const sources = selectors === null ? [undefined] : selectors;
    for (const source of sources) {
      const form: AgentPolicyGrantForm = {
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
 * The dimensions the editor's draft can hold and reproduce exactly.
 *
 * Narrower than `ALLOWED_SELECTOR_KEYS`: the server also accepts `server_url`
 * and `server_identity` on risk policy selectors, but the editor has no control
 * for them and the draft selector has nowhere to put them. A grant carrying one
 * is preserved verbatim rather than rewritten — see `agentPolicyViewFromGrants`.
 */
const DRAFT_DIMENSIONS: Record<string, readonly string[]> = {
  mcp: ["projectId", "disposition", "tool"],
  environment: ["projectId"],
};

/**
 * The dimensions a selector actually constrains. A wildcard constrains nothing,
 * so it reads the same as an absent key — which is how `Selector.Matches` in
 * server/internal/authz/selector.go treats it.
 */
function constrainedDimensions(
  selector: AgentPolicySelector,
): Record<string, string> {
  const constrained: Record<string, string> = {};
  for (const [key, value] of Object.entries(selector)) {
    if (key === "resourceKind" || key === "resourceId") continue;
    if (typeof value !== "string" || value === ANY_RESOURCE) continue;
    constrained[key] = value;
  }
  return constrained;
}

/**
 * Whether the editor can hold this grant and write it back unchanged.
 *
 * A grant that constrains a dimension the draft cannot carry would come back
 * out of the editor broader than it went in, and the save path replaces a
 * changed grant rather than updating it — so an edit to an unrelated permission
 * would silently drop the constraint. Such grants are never put in the draft.
 */
export function isAgentPolicyGrantRepresentable(
  grant: AgentPolicyGrant,
): boolean {
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
    if (
      selector.resourceId === ANY_RESOURCE &&
      Object.keys(constrainedDimensions(selector)).length === 0
    ) {
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
        ...(selector.projectId ? { projectId: selector.projectId } : {}),
        ...(selector.disposition ? { disposition: selector.disposition } : {}),
        ...(selector.tool ? { tool: selector.tool } : {}),
      },
    ];
  }
  return draft;
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
 * Split the stored ceiling into what the editor may rewrite and what it must
 * leave alone.
 *
 * A scope with even one unrepresentable grant is locked whole. Editing half of
 * a scope would let a save remove the constrained grant and re-add a broader
 * one under the same scope, which is the silent widening this split exists to
 * prevent.
 */
export function agentPolicyViewFromGrants(
  grants: AgentPolicyGrant[],
): AgentPolicyView {
  const locked = new Set(
    grants
      .filter((grant) => !isAgentPolicyGrantRepresentable(grant))
      .map((grant) => grant.scope),
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

export interface AgentPolicyDiff {
  create: AgentPolicyGrantForm[];
  remove: AgentPolicyGrant[];
}

/**
 * What to send to reach `next` from the stored ceiling. Grant identity is the
 * scope and selector together, so a changed permission is a removal and an
 * addition rather than an update.
 */
export function diffAgentPolicyGrants(
  stored: AgentPolicyGrant[],
  next: AgentPolicyGrantForm[],
): AgentPolicyDiff {
  const storedByKey = new Map(
    stored.map((grant) => [
      delegableGrantKey({
        effect: "allow",
        scope: grant.scope,
        selector: grant.selector,
      }),
      grant,
    ]),
  );
  const nextKeys = new Set(next.map(delegableGrantKey));
  return {
    create: next.filter((form) => !storedByKey.has(delegableGrantKey(form))),
    remove: [...storedByKey]
      .filter(([key]) => !nextKeys.has(key))
      .map(([, grant]) => grant),
  };
}

/**
 * Invalidate everything a changed ceiling makes stale.
 *
 * The delegable candidate set matters most: it is what the API key dialog
 * narrows, it is keyed by the authorizing user because it is an intersection
 * with *their* live permissions, and leaving it cached is what makes a
 * freshly permitted agent still look unusable.
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
