import { AgentPermissionEditor } from "@/pages/agents/AgentPermissionEditor";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { useSlugs } from "@/contexts/Sdk";
import { ResourceLink } from "@/pages/access/ResourceLink";
import {
  useResourceLinkMaps,
  type ResourceLinkMaps,
} from "@/pages/access/useResourceLinkMaps";
import { CONNECT_SCOPE } from "@/pages/agents/agentPolicyEdit";
import {
  isAgentCredentialScope,
  agentPolicyBroadGrantLabel,
  isAgentPolicyConstraint,
} from "@/pages/agents/agent-policy-grants";
import { useAgentPolicyScopes } from "@/pages/agents/useAgentPolicyScopes";
import { ANY_RESOURCE } from "@/pages/agents/agent-api-key-grants";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import { useRoutes } from "@/routes";
import { Text } from "@/components/ui/Text";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { GrantOnBehalfOf } from "./GrantOnBehalfOf";
import { useAgentPolicyGrants } from "./useAgentPolicyGrants";
import { Link } from "react-router";
import { encodeIdentityUrn } from "@/lib/identity-urn";

/**
 * Every narrowing beyond the resource itself, including constraints the
 * permission editor preserves. The resource is rendered as a link beside it.
 */
function agentSelectorNarrowing(
  selector: AgentPolicySelector,
  project: { id: string; name?: string } | undefined,
): string {
  const parts: string[] = [];
  // A project the grant stores is shown even unnamed; one read from its
  // server only when it has a name, so a row never gains a bare id.
  if (project && (project.name || isAgentPolicyConstraint(selector.projectId)))
    parts.push(`Project: ${project.name || project.id}`);
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

/** Scopes whose grants also satisfy mcp:connect on the servers they name. */
const MCP_SCOPES_THAT_CONNECT = new Set(["mcp:read", "mcp:write"]);

/**
 * The project a grant's resource is in. A grant on one MCP server may leave
 * its project out: the setup flow stores the server with its project, the
 * permission picker stores the server alone, and the gateway enforces both
 * the same way, since a server lives in exactly one project. Read from the
 * server itself, every server row names its project, however it was saved.
 */
function grantProjectId(
  selector: AgentPolicySelector,
  maps: ResourceLinkMaps,
): string | undefined {
  if (isAgentPolicyConstraint(selector.projectId)) return selector.projectId;
  if (selector.resourceKind !== "mcp") return undefined;
  if (!isAgentPolicyConstraint(selector.resourceId)) return undefined;
  return (
    maps.toolsetMap.get(selector.resourceId)?.projectId ??
    maps.mcpServerMap.get(selector.resourceId)?.projectId
  );
}

/** One scope's grants, each with what it reaches and how it is narrowed. */
function ScopeGrants({
  agentId,
  scope,
  grants,
  maps,
}: {
  agentId: string;
  scope: string;
  grants: AgentPolicyGrant[];
  maps: ResourceLinkMaps;
}): JSX.Element {
  const { orgSlug } = useSlugs();
  const { catalogue } = useAgentPolicyScopes();
  // `Text` with no children is a loading skeleton, so a scope the catalogue
  // does not describe (e.g. `mcp:blocked_*`, or any scope while it loads)
  // must skip the line rather than pass `undefined`.
  const entry = catalogue.get(scope);
  const description = entry?.description;
  return (
    <div className="border-border space-y-1 border-b px-4 py-3 last:border-b-0">
      {/* "Run inference (inference:execute)", as scopeTitle reads; the slug
          keeps its own span so it stays monospaced. */}
      <Text>
        {entry?.label ? `${entry.label} (` : ""}
        <span className="font-mono">{scope}</span>
        {entry?.label ? ")" : ""}
      </Text>
      {description && (
        <Text small muted>
          {description}
        </Text>
      )}
      <ul className="marker:text-muted-foreground list-disc space-y-1 pt-1 pl-5">
        {grants.map((grant) => {
          const { selector } = grant;
          const projectId = grantProjectId(selector, maps);
          // The same words the permission editor's list of changes uses.
          const broad = agentPolicyBroadGrantLabel(selector);
          const narrowing = agentSelectorNarrowing(
            selector,
            // Presence, not truthiness: an empty stored project still narrows.
            projectId === undefined
              ? undefined
              : { id: projectId, name: maps.projectMap.get(projectId)?.name },
          );
          return (
            <li key={grant.id} className="pl-1">
              <div className="inline-flex flex-wrap items-center gap-x-2 gap-y-1 align-top">
                {broad !== undefined ? (
                  <Text small>{broad}</Text>
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
                {/* A server the agent signs in to with a person's own
                    connection is used on that person's behalf. */}
                {grant.scope === CONNECT_SCOPE &&
                  selector.resourceKind === "mcp" &&
                  selector.resourceId !== ANY_RESOURCE && (
                    <GrantOnBehalfOf
                      agentId={agentId}
                      resourceId={selector.resourceId}
                    />
                  )}
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
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
  // Shared with the editor: saving there invalidates this view immediately.
  const grants = useAgentPolicyGrants(agent);
  const { isError: catalogueError } = useAgentPolicyScopes();
  // Credential rules are listed and edited in their own panel.
  const visible = canRead
    ? (grants.data ?? []).filter(
        (grant) => !isAgentCredentialScope(grant.scope),
      )
    : [];
  // The servers the agent reaches come first, in a section of their own:
  // they decide what it can do at all, and they are edited in the agent's
  // setup flow, beside the sign-in each server needs.
  const servers = visible.filter((grant) => grant.scope === CONNECT_SCOPE);
  const rows = visible.filter((grant) => grant.scope !== CONNECT_SCOPE);
  // mcp:read and mcp:write also satisfy mcp:connect. They are edited with the
  // other permissions, so they stay listed there, but the server section
  // must not say the agent cannot connect while they let it.
  const connectingScopes = [
    ...new Set(
      rows
        .filter(
          (grant) =>
            MCP_SCOPES_THAT_CONNECT.has(grant.scope) &&
            grant.selector.resourceKind === "mcp",
        )
        .map((grant) => grant.scope),
    ),
  ];
  // Only a grant naming a specific MCP server needs the org inventories;
  // project names come from the session.
  const maps = useResourceLinkMaps(
    visible.some(
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
        {servers.length > 0
          ? "No other permissions configured for this agent."
          : "No permissions configured for this agent."}
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
        {catalogueError && (
          <Text small muted className="px-4 pt-3">
            Permission names could not be loaded, so only their identifiers are
            shown.
          </Text>
        )}
        {[...byScope].map(([scope, scopeGrants]) => (
          <ScopeGrants
            key={scope}
            agentId={agent.id}
            scope={scope}
            grants={scopeGrants}
            maps={maps}
          />
        ))}
      </>
    );
  }
  // One panel says when the policy cannot be read or loaded; a second one
  // saying the same thing adds nothing.
  const showServers = canRead && !(grants.isError && visible.length === 0);
  return (
    <>
      {showServers && (
        <IdentityPanel
          title="MCP servers"
          action={
            agent.permissions.write && (
              <Button asChild variant="secondary" size="sm">
                {/* The MCP sign-in tab, where servers are picked together
                    with how the agent signs in to each. */}
                <Link
                  to={routes.identities.detail.mcpSignIn.href(
                    encodeIdentityUrn(`agent:${agent.id}`),
                  )}
                >
                  Change servers
                </Link>
              </Button>
            )
          }
          loading={grants.isLoading}
          refreshFailed={grants.isError && visible.length > 0}
          onRetry={() => void grants.refetch()}
        >
          {servers.length > 0 && (
            <ScopeGrants
              agentId={agent.id}
              scope={CONNECT_SCOPE}
              grants={servers}
              maps={maps}
            />
          )}
          {servers.length === 0 && connectingScopes.length === 0 && (
            <IdentityPanelEmpty>
              This agent cannot connect to any MCP server.
            </IdentityPanelEmpty>
          )}
          {connectingScopes.length > 0 && (
            <Text small muted className="px-4 py-3">
              Its {connectingScopes.join(" and ")} permissions, listed with its
              other permissions, also let it connect to the servers they name.
            </Text>
          )}
        </IdentityPanel>
      )}
      <IdentityPanel
        title={servers.length > 0 ? "Other permissions" : "Agent permissions"}
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
        error={canRead && grants.isError && visible.length === 0}
        refreshFailed={canRead && grants.isError && visible.length > 0}
        onRetry={() => void grants.refetch()}
        footer="These configured permissions limit what credentials may delegate. Actual access also depends on the credential's grants, the owner's current permissions, and the agent's status."
      >
        {content}
        {editing && canRead && grants.data && (
          <AgentPermissionEditor
            agent={agent}
            grants={grants.data}
            onClose={() => setEditing(false)}
          />
        )}
      </IdentityPanel>
    </>
  );
}
