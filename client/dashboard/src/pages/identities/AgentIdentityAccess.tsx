import { registeredAgentHref } from "./identityRoster";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useOrgMcpServers } from "@/pages/access/useOrgMcpServers";
import { AGENT_POLICY_SCOPES } from "@/pages/agents/agent-policy-grants";
import { useRoutes } from "@/routes";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/Text";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";

/** Show every narrowing, including constraints the permission editor preserves. */
function agentSelectorDetails(
  selector: AgentPolicySelector,
  resourceName?: string,
): string {
  const resource =
    selector.resourceId === "*"
      ? "All resources"
      : resourceName || selector.resourceId;
  const parts = [resource];
  if (selector.projectId) parts.push(`Project: ${selector.projectId}`);
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
  const organization = useOrganization();
  const sdk = useSdkClient();
  const routes = useRoutes();
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
  const servers = useOrgMcpServers(
    canRead &&
      (grants.data ?? []).some(
        (grant) =>
          grant.selector.resourceKind === "mcp" &&
          grant.selector.resourceId !== "*",
      ),
  );
  const resourceNames = new Map(
    organization.projects.map((project) => [
      project.id,
      project.name || project.slug,
    ]),
  );
  for (const group of servers.groups) {
    for (const server of group.servers)
      resourceNames.set(server.id, `${server.name} (${group.projectName})`);
  }
  const rows = canRead ? (grants.data ?? []) : [];
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
    content = (
      <>
        {rows.map((grant) => (
          <div
            key={grant.id}
            className="border-border space-y-1 border-b px-4 py-3 last:border-b-0"
          >
            <Text className="font-mono">{grant.scope}</Text>
            <Text small muted>
              {
                AGENT_POLICY_SCOPES.find((scope) => scope.slug === grant.scope)
                  ?.description
              }
            </Text>
            <Text small className="break-words">
              {agentSelectorDetails(
                grant.selector,
                resourceNames.get(grant.selector.resourceId),
              )}
            </Text>
          </div>
        ))}
      </>
    );
  }
  return (
    <IdentityPanel
      title="Agent permissions"
      handoffLabel="Agent Identity"
      handoffHref={registeredAgentHref(routes.agents.href(), "", agent.id)}
      loading={canRead && grants.isLoading}
      error={canRead && grants.isError && rows.length === 0}
      refreshFailed={canRead && grants.isError && rows.length > 0}
      onRetry={() => void grants.refetch()}
      footer="These configured permissions limit what credentials may delegate. Actual access also depends on the credential's grants, the owner's current permissions, and the agent's status."
    >
      {content}
    </IdentityPanel>
  );
}
