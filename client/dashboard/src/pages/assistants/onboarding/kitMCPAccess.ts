import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  buildAccessRows,
  effectiveForeignGrants,
  scopeState,
} from "@/pages/mcp/access/accessRows";

/** Only edit full Connect grants for this server. A wildcard, narrowed rule,
 * or another scope belongs to the normal access editor. */
export function directServerConnectGrants(
  grants: AgentPolicyGrant[],
  projectId: string,
  resourceId: string,
): AgentPolicyGrant[] {
  return grants.filter(
    ({ effect, scope, selector }) =>
      effect === "allow" &&
      scope === "mcp:connect" &&
      selector.resourceKind === "mcp" &&
      selector.resourceId === resourceId &&
      (selector.projectId === undefined || selector.projectId === projectId) &&
      Object.entries(selector).every(
        ([key, value]) =>
          value === undefined ||
          key === "resourceKind" ||
          key === "resourceId" ||
          key === "projectId",
      ),
  );
}

export function agentConnectAccess(
  entries: ResourceAudienceEntry[],
  agentId: string,
): string {
  const principalUrn = `agent:${agentId}`;
  const [row] = buildAccessRows(entries, [
    { principalUrn, kind: "agent", displayName: "Agent" },
  ]);
  if (!row || row.accessUnknown) return "Access unknown";
  const state = scopeState(row, "use");
  if (state.unset) return "No access";
  if (!state.granted && state.via) return `Blocked by ${state.via}`;
  if (!state.granted) return state.value;

  const inherited = [
    ...new Set(
      effectiveForeignGrants(row, "use").flatMap((grant) => {
        if (grant.principalUrn !== principalUrn) return [grant.displayName];
        if (grant.appliesTo === "all_resources")
          return ["a broader agent grant"];
        return [];
      }),
    ),
  ];
  const details = [
    inherited.length > 0 ? `via ${inherited.join(", ")}` : state.note,
    state.capped ? "limited by access rules" : undefined,
  ].filter(Boolean);
  return [state.value, ...details].join(" · ");
}
