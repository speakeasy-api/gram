import { AgentPermissionEditor } from "@/pages/agents/AgentPermissionEditor";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useAllAuthClients } from "@/lib/useAllAuthClients";
import { ResourceLink } from "@/pages/access/ResourceLink";
import { useResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import { CONNECT_SCOPE } from "@/pages/agents/agentPolicyEdit";
import {
  AGENT_CREDENTIAL_BLOCK_SCOPE,
  agentPolicyUnrestrictedLabel,
  isAgentPolicyConstraint,
  isAgentPolicySelectorUnrestricted,
} from "@/pages/agents/agent-policy-grants";
import { useAgentPolicyScopes } from "@/pages/agents/useAgentPolicyScopes";
import { ANY_RESOURCE } from "@/pages/agents/agent-api-key-grants";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicySelector } from "@gram/client/models/components/agentpolicyselector.js";
import { useQueries, useQuery } from "@tanstack/react-query";
import { queryKeyOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import { useRoutes } from "@/routes";
import { Text } from "@/components/ui/Text";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { GrantOnBehalfOf } from "./GrantOnBehalfOf";
import { useAgentPolicyGrants } from "./useAgentPolicyGrants";
import {
  authGrantParts,
  authGrantRank,
  authGrantText,
  REPRESENTED_USER,
  type AuthGrantNames,
  type AuthGrantPart,
  type AuthGrantServer,
} from "./auth-grant-label";
import { IdentityLink } from "@/components/identity-link";
import { serverLabel } from "@/lib/auth-clients";
import { mcpServerRouteParam } from "@/lib/sources";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { Link } from "react-router";

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

/** Scopes whose grants also satisfy mcp:connect on the servers they name. */
const MCP_SCOPES_THAT_CONNECT = new Set(["mcp:read", "mcp:write"]);

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

/**
 * Descriptions for scopes the permission editor does not offer, so a stored
 * grant on one still says what it allows.
 */
const LOCKED_SCOPE_DESCRIPTIONS: Record<string, string> = {
  "mcp:auth": "Sign in to MCP servers through these upstream credentials.",
};

/**
 * A scope's grants in reading order. `mcp:auth` grants go broadest first, down
 * to one named credential, then by label; other scopes keep the policy's order.
 * A grant on any upstream credential covers every narrower one, so it is
 * shown alone.
 */
function orderGrants(
  scope: string,
  grants: AgentPolicyGrant[],
  names: AuthGrantNames,
): AgentPolicyGrant[] {
  if (scope !== "mcp:auth") return grants;
  const wildcard = grants.find(
    (grant) =>
      grant.effect === "allow" &&
      isAgentPolicySelectorUnrestricted(grant.selector),
  );
  if (wildcard) {
    return [wildcard, ...grants.filter((grant) => grant.effect !== "allow")];
  }
  const label = (grant: AgentPolicyGrant) =>
    authGrantText(authGrantParts(grant.selector, names));
  return [...grants].sort(
    (a, b) =>
      authGrantRank(a.selector) - authGrantRank(b.selector) ||
      label(a).localeCompare(label(b)),
  );
}

function authGrantServer(
  server: AuthClient["servers"][number],
): AuthGrantServer {
  return {
    id: server.id,
    slug: server.slug,
    name: serverLabel(server),
    projectId: server.projectId,
  };
}

/**
 * A grant's credential label, with each person linked to their identity page,
 * each upstream OAuth client to its page, and each server to its
 * authentication settings, where the credential lives.
 */
function AuthGrantLabel({ parts }: { parts: AuthGrantPart[] }): JSX.Element {
  const organization = useOrganization();
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const projectSlug = (projectId: string) =>
    organization.projects?.find((project) => project.id === projectId)?.slug;
  return (
    <Text small>
      {parts.map((part, index) => {
        if (part.kind === "text") return part.text;
        if (part.kind === "person") {
          return (
            <IdentityLink key={index} identifier={{ userId: part.userId }}>
              {part.name}
            </IdentityLink>
          );
        }
        if (part.kind === "upstream") {
          if (!part.issuerId) return part.name;
          return (
            <Link
              key={index}
              to={routes.remoteIdentityProviders.clientDetail.href(
                part.issuerId,
                part.id,
              )}
              className="underline-offset-2 hover:underline"
            >
              {part.name}
            </Link>
          );
        }
        return part.servers.map((server, serverIndex) => {
          const slug = projectSlug(server.projectId);
          const separator = serverIndex > 0 ? ", " : "";
          if (!slug) {
            return `${separator}${server.name}`;
          }
          return (
            <span key={`${index}-${server.id}`}>
              {separator}
              <Link
                to={`/${orgSlug}/projects/${slug}/mcp/${mcpServerRouteParam(server)}/settings#authentication`}
                className="underline-offset-2 hover:underline"
              >
                {server.name}
              </Link>
            </span>
          );
        });
      })}
    </Text>
  );
}

/**
 * Names for the auth clients, upstream OAuth clients and people that the
 * agent's `mcp:auth` grants point at. Both lists need org:read and load only
 * when a grant needs them; without them the labels fall back to short ids.
 */
function useAuthGrantNames(grants: AgentPolicyGrant[]): AuthGrantNames {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const authGrants = grants.filter(
    ({ selector }) => selector.resourceKind === "mcp_auth_client",
  );
  const needsMembers = authGrants.some(
    ({ selector }) =>
      isAgentPolicyConstraint(selector.connectionOwner) &&
      selector.connectionOwner !== REPRESENTED_USER,
  );
  // Only a grant naming an auth client or an upstream OAuth client needs the
  // organization's auth clients; wildcard and type-only grants name neither.
  const needsClients = authGrants.some(
    ({ selector }) =>
      isAgentPolicyConstraint(selector.resourceId) ||
      isAgentPolicyConstraint(selector.upstreamClient),
  );
  const clients = useAllAuthClients({
    enabled: needsClients && hasScope("org:read"),
  });
  const upstreamIds = [
    ...new Set(
      authGrants.flatMap(({ selector }) =>
        isAgentPolicyConstraint(selector.upstreamClient)
          ? [selector.upstreamClient]
          : [],
      ),
    ),
  ];
  // Each upstream client a grant names, for its issuer (its page) and its own
  // name when no auth client signs in through it.
  const upstreamClients = useQueries({
    queries: upstreamIds.map((id) => ({
      queryKey: queryKeyOrganizationRemoteSessionClient({ id }),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.organizationRemoteSessionClients.get({ id }, undefined, {
          signal,
        }),
      enabled: hasScope("org:read"),
      retry: false,
      throwOnError: false,
    })),
  });
  const members = useQuery({
    queryKey: ["agent-auth-grant-members", organization.id],
    queryFn: ({ signal }) => sdk.access.listMembers({}, undefined, { signal }),
    enabled: needsMembers && hasScope("org:read"),
    retry: false,
    throwOnError: false,
  });
  const clientById = new Map(
    (clients.data ?? []).map((client) => [
      client.id,
      {
        name: client.name,
        clientType: client.clientType,
        servers: client.servers.map(authGrantServer),
      },
    ]),
  );
  const upstreamClientById = new Map(
    upstreamClients.flatMap(({ data }) => (data ? [[data.id, data]] : [])),
  );
  // An upstream OAuth client is named after the first auth client using it,
  // and reaches every server those auth clients serve.
  const upstreamById = new Map<
    string,
    { name: string; issuerId?: string; servers: AuthGrantServer[] }
  >();
  for (const client of clients.data ?? []) {
    if (!client.remoteSessionClientId) continue;
    const seen = upstreamById.get(client.remoteSessionClientId) ?? {
      name: client.name,
      issuerId: upstreamClientById.get(client.remoteSessionClientId)
        ?.remoteSessionIssuerId,
      servers: [],
    };
    for (const server of client.servers) {
      if (seen.servers.some(({ id }) => id === server.id)) continue;
      seen.servers.push(authGrantServer(server));
    }
    upstreamById.set(client.remoteSessionClientId, seen);
  }
  // One no auth client signs in through any more keeps its own name.
  for (const upstream of upstreamClientById.values()) {
    if (upstreamById.has(upstream.id)) continue;
    upstreamById.set(upstream.id, {
      name: remoteSessionClientDisplayName(upstream),
      issuerId: upstream.remoteSessionIssuerId,
      servers: [],
    });
  }
  const memberById = new Map(
    (members.data?.members ?? []).map((member) => [
      member.id,
      member.name || member.email,
    ]),
  );
  return {
    client: (id) => clientById.get(id),
    upstreamClient: (id) => upstreamById.get(id),
    member: (id) => memberById.get(id),
    clientsLoaded: clients.isSuccess,
  };
}

/** One scope's grants, each with what it reaches and how it is narrowed. */
function ScopeGrants({
  agentId,
  scope,
  grants,
  maps,
  authNames,
}: {
  agentId: string;
  scope: string;
  grants: AgentPolicyGrant[];
  maps: ReturnType<typeof useResourceLinkMaps>;
  authNames: AuthGrantNames;
}): JSX.Element {
  const { orgSlug } = useSlugs();
  const { catalogue } = useAgentPolicyScopes();
  // `Text` with no children is a loading skeleton, so a scope the catalogue
  // does not describe (e.g. `mcp:blocked_*`, or any scope while it loads)
  // must skip the line rather than pass `undefined`.
  const entry = catalogue.get(scope);
  const description = entry?.description ?? LOCKED_SCOPE_DESCRIPTIONS[scope];
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
        {orderGrants(scope, grants, authNames).map((grant) => {
          const { selector } = grant;
          const narrowing = agentSelectorNarrowing(
            selector,
            selector.projectId
              ? maps.projectMap.get(selector.projectId)?.name
              : undefined,
          );
          return (
            <li key={grant.id} className="pl-1">
              <div className="inline-flex flex-wrap items-center gap-x-2 gap-y-1 align-top">
                {selector.resourceKind === "mcp_auth_client" ? (
                  <AuthGrantLabel parts={authGrantParts(selector, authNames)} />
                ) : selector.resourceId === ANY_RESOURCE ? (
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
  // Credential restrictions are listed and edited in their own panel.
  const visible = canRead
    ? (grants.data ?? []).filter(
        (grant) => grant.scope !== AGENT_CREDENTIAL_BLOCK_SCOPE,
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
  const authNames = useAuthGrantNames(rows);
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
            authNames={authNames}
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
            <Button asChild variant="secondary" size="sm">
              {/* The setup flow's server step, where servers are picked
                  together with how the agent signs in to each. */}
              <Link
                to={`${routes.identities.agents.edit.href(agent.id)}?step=1`}
              >
                Edit servers
              </Link>
            </Button>
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
              authNames={authNames}
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
