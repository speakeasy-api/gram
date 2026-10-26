import { AgentPermissionEditor } from "@/pages/agents/AgentPermissionEditor";
import {
  useAgentPolicyGrants,
  useKeysMissingPermissions,
} from "@/pages/agents/useAgentKeyPermissions";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { Link } from "react-router";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { ResourceLink } from "@/pages/access/ResourceLink";
import { useResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import { AGENT_POLICY_SCOPES } from "@/pages/agents/agent-policy-grants";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
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
  if (selector.projectId)
    parts.push(`Project: ${projectName || selector.projectId}`);
  if (selector.tool) parts.push(`Tool: ${selector.tool}`);
  if (selector.disposition) parts.push(`Disposition: ${selector.disposition}`);
  if (selector.serverIdentity)
    parts.push(`Server identity: ${selector.serverIdentity}`);
  if (selector.serverUrl) parts.push(`Server URL: ${selector.serverUrl}`);
  return parts.join(" · ");
}

export function AgentIdentityPermissions({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const routes = useRoutes();
  const [editing, setEditing] = useState(false);
  // The policy endpoint requires setup/write access, independently of agent:read.
  const canRead = agent.permissions.write;
  const grants = useAgentPolicyGrants(agent);
  // Saving an added permission refreshes the policy, so this names the keys
  // left behind as soon as the editor closes.
  const outdated = useKeysMissingPermissions(agent);
  const { orgSlug } = useSlugs();
  const rows = canRead ? (grants.data ?? []) : [];
  // Only a grant naming a specific MCP server needs the org inventories;
  // project names come from the session.
  const maps = useResourceLinkMaps(
    rows.some(
      ({ selector }) =>
        selector.resourceKind === "mcp" && selector.resourceId !== "*",
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
                      {selector.resourceId === "*" ? (
                        <Text small>All resources</Text>
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
      footer="A request is allowed only what the key, these permissions and the owner's current permissions all allow, while the agent is active. Each key keeps the permissions it was issued with: removing one here applies to every key at once, but adding one reaches a key only when it is reissued."
    >
      {outdated.length > 0 && (
        <div className="border-border border-b px-4 py-3">
          <Alert
            variant="warning"
            title={
              outdated.length === 1
                ? `${outdated[0]!.name} can't use permissions added since it was issued`
                : `${outdated.length} keys can't use permissions added since they were issued`
            }
          >
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span>
                Keys keep the permissions they were issued with. Reissue{" "}
                {outdated.length === 1 ? "it" : "them"} to include the new ones.
              </span>
              <Button asChild size="sm" variant="secondary">
                <Link
                  to={routes.identities.detail.credentials.href(
                    encodeIdentityUrn(`agent:${agent.id}`),
                  )}
                >
                  Review keys
                </Link>
              </Button>
            </div>
          </Alert>
        </div>
      )}
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
