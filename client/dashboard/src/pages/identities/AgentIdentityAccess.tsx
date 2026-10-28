import { AgentPermissionEditor } from "@/pages/agents/AgentPermissionEditor";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { ResourceLink } from "@/pages/access/ResourceLink";
import { useResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import {
  AGENT_POLICY_SCOPES,
  agentPolicyUnrestrictedLabel,
  isAgentPolicyConstraint,
  isAgentPolicySelectorUnrestricted,
} from "@/pages/agents/agent-policy-grants";
import { ANY_RESOURCE } from "@/pages/agents/agent-api-key-grants";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/Text";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";

/**
 * Every narrowing beyond the resource itself, including constraints the
 * permission editor preserves. The resource is rendered as a link beside it.
 */
function agentSelectorNarrowing(
  selector: AgentPolicySelector,
  projectName?: string,
): string {
  const parts: string[] = [];
  if (isAgentPolicyConstraint(selector.projectId))
    parts.push(`Project: ${projectName || selector.projectId}`);
  if (isAgentPolicyConstraint(selector.tool))
    parts.push(`Tool: ${selector.tool}`);
  if (isAgentPolicyConstraint(selector.disposition))
    parts.push(`Disposition: ${selector.disposition}`);
  if (isAgentPolicyConstraint(selector.serverIdentity))
    parts.push(`Server identity: ${selector.serverIdentity}`);
  if (isAgentPolicyConstraint(selector.serverUrl))
    parts.push(`Server URL: ${selector.serverUrl}`);
  return parts.join(" · ");
}

/**
 * What a grant on every resource of its kind reaches. An org grant still
 * narrowed, e.g. to a project, is not the whole organization.
 */
function wildcardGrantLabel(selector: AgentPolicySelector): string {
  if (!isAgentPolicySelectorUnrestricted(selector)) {
    return agentPolicyUnrestrictedLabel(undefined);
  }
  return agentPolicyUnrestrictedLabel(selector.resourceKind);
}

export function AgentIdentityPermissions({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const [editing, setEditing] = useState(false);
  // The policy endpoint requires setup/write access, independently of agent:read.
  const canRead = agent.permissions.write;
  const grants = useQuery({
    // Shared with the editor: saving there invalidates this view immediately.
    queryKey: ["agent-policy-grants", organization.id, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    enabled: canRead,
    retry: false,
    throwOnError: false,
  });
  const { orgSlug } = useSlugs();
  const rows = canRead ? (grants.data ?? []) : [];
  // Only a grant naming a specific MCP server needs the org inventories;
  // project names come from the session.
  const maps = useResourceLinkMaps(
    rows.some(
      ({ selector }) =>
        selector.resourceKind === "mcp" && selector.resourceId !== ANY_RESOURCE,
    ),
  );
  let content: JSX.Element;
  if (!canRead) {
    content = (
      <IdentityPanelEmpty>
        You need permission to manage this agent to view its policy.
      </IdentityPanelEmpty>
    );
  } else if (rows.length === 0) {
    content = (
      <IdentityPanelEmpty>
        No permissions configured for this agent.
      </IdentityPanelEmpty>
    );
  } else {
    // One row per scope, listing what it reaches: the policy stores a grant
    // per resource, and a row each repeated the scope and its description
    // once for every server.
    const byScope = new Map<string, AgentPolicyGrant[]>();
    for (const grant of rows) {
      const group = byScope.get(grant.scope);
      if (group) group.push(grant);
      else byScope.set(grant.scope, [grant]);
    }
    content = (
      <>
        {[...byScope].map(([scope, grants]) => {
          // `Text` with no children is a loading skeleton, so a scope the
          // mirror does not describe (e.g. `mcp:blocked_*`) must skip the
          // line rather than pass `undefined`.
          const description = AGENT_POLICY_SCOPES.find(
            (definition) => definition.slug === scope,
          )?.description;
          return (
            <div
              key={scope}
              className="border-border space-y-1 border-b px-4 py-3 last:border-b-0"
            >
              <Text className="font-mono">{scope}</Text>
              {description && (
                <Text small muted>
                  {description}
                </Text>
              )}
              <ul className="flex flex-col gap-1 pt-1">
                {grants.map((grant) => {
                  const { selector } = grant;
                  const narrowing = agentSelectorNarrowing(
                    selector,
                    selector.projectId
                      ? maps.projectMap.get(selector.projectId)?.name
                      : undefined,
                  );
                  return (
                    <li
                      key={grant.id}
                      className="flex flex-wrap items-center gap-x-2 gap-y-1"
                    >
                      {selector.resourceId === ANY_RESOURCE ? (
                        <Text small>{wildcardGrantLabel(selector)}</Text>
                      ) : (
                        <ResourceLink
                          challenge={{
                            resourceKind: selector.resourceKind,
                            resourceId: selector.resourceId,
                            projectId: selector.projectId,
                          }}
                          orgSlug={orgSlug ?? ""}
                          {...maps}
                        />
                      )}
                      {narrowing && (
                        <Text small muted className="break-words">
                          {narrowing}
                        </Text>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          );
        })}
      </>
    );
  }
  return (
    <IdentityPanel
      title="Agent permissions"
      action={
        canRead && grants.data && !grants.isError ? (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setEditing(true)}
          >
            Edit permissions
          </Button>
        ) : undefined
      }
      loading={canRead && grants.isLoading}
      error={canRead && grants.isError && rows.length === 0}
      refreshFailed={canRead && grants.isError && rows.length > 0}
      onRetry={() => void grants.refetch()}
      footer="These configured permissions limit what credentials may delegate. Actual access also depends on the credential's grants, the owner's current permissions, and the agent's status."
    >
      {content}
      {editing && canRead && grants.data && (
        <AgentPermissionEditor
          agent={agent}
          grants={rows}
          onClose={() => setEditing(false)}
        />
      )}
    </IdentityPanel>
  );
}
