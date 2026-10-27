import { useMembers } from "@gram/client/react-query/members.js";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { sessionAccountLabel } from "@/components/sessions/session-account-identity";
import {
  connectionLifetime,
  connectionLifetimeLabel,
} from "@/components/sessions/connection-lifetime";
import { useEffect, useRef, useState, type JSX } from "react";
import { useQueries } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Server, CheckCircle2, ExternalLink, LockKeyhole } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  internalMcpUrl,
  customDomainMcpEndpointUrl,
} from "@/hooks/useToolsetUrl";
import { firstPartyConnectUrl, getServerURL } from "@/lib/utils";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { queryKeyRemoteSessions } from "@gram/client/react-query/remoteSessions.js";
import { queryKeyRemoteSessionsListBindings } from "@gram/client/react-query/remoteSessionsListBindings.js";
import { queryKeyRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useRemoteSessionsAttachBindingMutation } from "@gram/client/react-query/remoteSessionsAttachBinding.js";
import { useRemoteSessionsDetachBindingMutation } from "@gram/client/react-query/remoteSessionsDetachBinding.js";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";

import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { narrowGrantsToServers } from "./agent-key-server-grants";
export interface KeyServer {
  id: string;
  resourceId: string;
  name: string;
  projectId: string;
  projectSlug: string;
  kind: string;
  toolsetSlug?: string;
  issuerId?: string;
  endpoints?: string[];
  connectUrl?: string;
  unavailable?: boolean;
}

export function AgentKeyServers({
  agent,
  step,
  selected,
  onChange,
  onReady,
  onBusy,
  onInventory,
  grants,
  discoveryComplete,
  discoveryError,
  onRetryDiscovery,
}: {
  grants: AgentPolicyGrantForm[];
  discoveryComplete: boolean;
  discoveryError?: boolean;
  onRetryDiscovery?: () => void;
  onInventory?: (servers: KeyServer[]) => void;
  agent: ManagedAgent;
  step: number;
  selected: KeyServer[];
  onChange: (servers: KeyServer[]) => void;
  onReady: (ready: boolean) => void;
  onBusy?: (busy: boolean) => void;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const toolsets = useListToolsetsForOrg(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const modern = useListMcpServersForOrg(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const [search, setSearch] = useState("");
  useEffect(() => {
    onBusy?.(
      pending ||
        !toolsets.isSuccess ||
        !modern.isSuccess ||
        toolsets.isFetching ||
        modern.isFetching ||
        toolsets.isError ||
        modern.isError,
    );
  }, [
    pending,
    toolsets.isSuccess,
    modern.isSuccess,
    toolsets.isFetching,
    modern.isFetching,
    toolsets.isError,
    modern.isError,
    onBusy,
  ]);
  const projects = new Map(organization.projects.map((p) => [p.id, p]));
  const modernToolsets = new Set(
    modern.data?.mcpServers.flatMap((s) =>
      s.toolsetId ? [s.toolsetId] : [],
    ) ?? [],
  );
  const inventory: KeyServer[] = [
    ...(modern.data?.mcpServers ?? []).map((s) => ({
      id: s.id,
      resourceId: s.toolsetId ?? s.id,
      name: s.name ?? s.slug ?? "Unnamed server",
      projectId: s.projectId,
      projectSlug: projects.get(s.projectId)?.slug ?? "",
      issuerId: s.userSessionIssuerId,
      unavailable: s.visibility === "disabled",
      kind: s.unproxiedMcpServerId
        ? "Unproxied"
        : s.tunneledMcpServerId
          ? "Tunneled"
          : s.remoteMcpServerId
            ? "Remote"
            : "Hosted",
    })),
    ...(toolsets.data?.toolsets ?? [])
      .filter((t) => t.mcpEnabled === true && !modernToolsets.has(t.id))
      .map((t) => ({
        id: t.id,
        resourceId: t.id,
        name: t.name,
        projectId: t.projectId,
        projectSlug: projects.get(t.projectId)?.slug ?? "",
        kind: "Toolset",
        toolsetSlug: t.slug,
      })),
  ];
  const supportedInventory = inventory.filter(
    (server) =>
      !server.unavailable &&
      server.kind !== "Unproxied" &&
      !!server.projectSlug,
  );
  const inventoryKey = JSON.stringify(
    toolsets.isSuccess &&
      modern.isSuccess &&
      !toolsets.isFetching &&
      !modern.isFetching &&
      !toolsets.isError &&
      !modern.isError
      ? supportedInventory
      : [],
  );
  useEffect(() => {
    onInventory?.(JSON.parse(inventoryKey));
  }, [inventoryKey, onInventory]);
  const visibleServers = (discoveryComplete ? supportedInventory : [])
    .filter((server) => narrowGrantsToServers(grants, [server]).length > 0)
    .filter((server) =>
      `${server.name} ${projects.get(server.projectId)?.name ?? ""}`
        .toLowerCase()
        .includes(search.trim().toLowerCase()),
    );
  async function toggle(server: KeyServer) {
    if (selected.some((s) => s.id === server.id)) {
      onChange(selected.filter((s) => s.id !== server.id));
      return;
    }
    setPending(true);
    setError(false);
    try {
      let entry: KeyServer;
      if (server.toolsetSlug) {
        const detail = await sdk.toolsets.getBySlug({
          slug: server.toolsetSlug,
          gramProject: server.projectSlug,
        });
        const url = internalMcpUrl({ slug: server.projectSlug }, detail);
        entry = {
          ...server,
          issuerId: detail.userSessionIssuerId,
          endpoints: url ? [url] : [],
          connectUrl: detail.mcpSlug
            ? firstPartyConnectUrl(`${getServerURL()}/mcp/${detail.mcpSlug}`)
            : undefined,
        };
      } else {
        const rows =
          server.kind === "Unproxied"
            ? []
            : (
                await sdk.mcpEndpoints.list({
                  mcpServerId: server.id,
                  gramProject: server.projectSlug,
                })
              ).mcpEndpoints;
        // Endpoint rows contain a slug/domain ID, not a resolved URL. Resolve
        // custom hosts from domain inventory, never reuse a custom slug on the
        // platform host. The configured platform base is authoritative.
        const domains = rows.some((e) => e.customDomainId)
          ? (await sdk.domains.listDomains()).domains
          : [];
        const endpoints = rows.flatMap((endpoint) => {
          if (!endpoint.slug) return [];
          if (!endpoint.customDomainId)
            return [`${getServerURL()}/mcp/${endpoint.slug}`];
          const domain = domains.find((d) => d.id === endpoint.customDomainId);
          return domain
            ? [customDomainMcpEndpointUrl(domain.domain, endpoint.slug)]
            : [];
        });
        const platform = rows.find((e) => !e.customDomainId && e.slug);
        entry = {
          ...server,
          endpoints,
          connectUrl: firstPartyConnectUrl(
            platform ? `${getServerURL()}/mcp/${platform.slug}` : undefined,
          ),
        };
      }
      onChange([...selected, entry]);
    } catch {
      setError(true);
    } finally {
      setPending(false);
    }
  }
  if (step === 1)
    return (
      <ServerAccounts agent={agent} servers={selected} onReady={onReady} />
    );
  if (discoveryError)
    return (
      <div role="alert">
        <Text>Could not load delegable permissions.</Text>
        <Button onClick={onRetryDiscovery}>Retry permissions</Button>
      </div>
    );
  if (toolsets.isError || modern.isError)
    return (
      <div role="alert">
        <Text>Could not load all MCP servers.</Text>
        <Button
          onClick={() => {
            void toolsets.refetch();
            void modern.refetch();
          }}
        >
          Retry servers
        </Button>
      </div>
    );
  if (
    !toolsets.isSuccess ||
    !modern.isSuccess ||
    toolsets.isFetching ||
    modern.isFetching ||
    (supportedInventory.length > 0 && !discoveryComplete)
  )
    return <Text>Loading MCP servers…</Text>;
  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Choose servers</h2>
        <Text small muted>
          The MCP servers this key may reach. The agent's gateway serves exactly
          these.
        </Text>
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex-1 sm:max-w-sm">
          <Input
            aria-label="Search servers"
            placeholder="Search servers…"
            value={search}
            onChange={setSearch}
          />
        </div>
        <Text small muted role="status">
          {selected.length} {selected.length === 1 ? "server" : "servers"}{" "}
          selected
        </Text>
      </div>
      <div className="divide-border border-border divide-y border">
        {visibleServers.map((server) => {
          const checked = selected.some((s) => s.id === server.id);
          const unavailable = !server.projectSlug || server.unavailable;
          return (
            <label
              key={server.id}
              className={cn(
                "flex items-center gap-4 p-4 transition-colors",
                checked ? "bg-accent/50" : "hover:bg-muted/40",
                unavailable || pending
                  ? "cursor-not-allowed"
                  : "cursor-pointer",
              )}
            >
              <Checkbox
                aria-label={server.name}
                checked={checked}
                disabled={pending || unavailable}
                onCheckedChange={() => void toggle(server)}
              />
              <Server
                aria-hidden="true"
                className="text-muted-foreground size-5 shrink-0"
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">
                  {server.name}
                </span>
                <Text as="span" small muted>
                  {projects.get(server.projectId)?.name ??
                    "Project unavailable"}
                </Text>
                {server.kind === "Unproxied" && (
                  <Text as="span" small muted className="block">
                    This server doesn’t accept Speakeasy API keys.
                  </Text>
                )}
              </span>
              {unavailable && <Badge variant="neutral">Unavailable</Badge>}
            </label>
          );
        })}
        {visibleServers.length === 0 && (
          <div className="space-y-2 px-6 py-10 text-center">
            <Text>
              {supportedInventory.length === 0
                ? "No servers available"
                : "No matching servers"}
            </Text>
            <Text small muted>
              {supportedInventory.length === 0
                ? "Add an MCP server to a project before creating a key."
                : "Try a different server or project name."}
            </Text>
          </div>
        )}
      </div>
      <Text small muted>
        Looking for a combined server? Choose its individual servers here.
      </Text>
      {pending && <Text role="status">Checking server connection…</Text>}
      {error && (
        <Text role="alert">
          Could not check this server’s connection. Select it again to retry.
        </Text>
      )}
    </div>
  );
}

export function ServerAccounts({
  agent,
  servers,
  onReady,
  compact = false,
}: {
  agent: ManagedAgent;
  compact?: boolean;
  servers: KeyServer[];
  onReady: (ready: boolean) => void;
}): JSX.Element | null {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const { data: members } = useMembers(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const required = [
    ...new Map(
      servers
        .filter((s) => s.issuerId)
        .map((s) => [`${s.projectId}:${s.issuerId}`, s]),
    ).values(),
  ];
  const [pending, setPending] = useState(false);
  const refreshInFlight = useRef(false);
  const [refreshing, setRefreshing] = useState<{
    bindingId: string;
    clientId: string;
    server: KeyServer;
  }>();
  const [error, setError] = useState(false);
  const attachBinding = useRemoteSessionsAttachBindingMutation();
  const detachBinding = useRemoteSessionsDetachBindingMutation();
  const ownerScope = { organizationId: organization.id, userId: user.id };
  const canManage = agent.permissions.write;
  const requests = required.map((server) => ({
    gramProject: server.projectSlug,
    principalId: agent.id,
    userSessionIssuerId: server.issuerId!,
  }));
  const candidatesQueries = useQueries({
    queries: requests.map((request) => ({
      queryKey: [...queryKeyRemoteSessions(request), ownerScope, "all-pages"],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        collectPageItems(
          sdk.remoteSessions.list(request, undefined, { signal }),
        ),
      retry: false,
      throwOnError: false,
    })),
  });
  const bindingsQueries = useQueries({
    queries: requests.map((request) => ({
      queryKey: [...queryKeyRemoteSessionsListBindings(request), ownerScope],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.remoteSessions.listBindings(request, undefined, { signal }),
      retry: false,
      throwOnError: false,
    })),
  });
  const clientsQueries = useQueries({
    queries: required.map((server) => {
      const request = {
        gramProject: server.projectSlug,
        userSessionIssuerId: server.issuerId!,
      };
      return {
        queryKey: [
          ...queryKeyRemoteSessionClients(request),
          ownerScope,
          "all-pages",
        ],
        queryFn: ({ signal }: { signal: AbortSignal }) =>
          collectPageItems(
            sdk.remoteSessionClients.list(request, undefined, { signal }),
          ),
        retry: false,
        throwOnError: false,
      };
    }),
  });
  const reads = [...candidatesQueries, ...bindingsQueries, ...clientsQueries];
  const query = {
    isSuccess: reads.every((read) => read.isSuccess),
    isLoading: reads.some((read) => read.isLoading),
    isFetching: reads.some((read) => read.isFetching),
    isError: reads.some((read) => read.isError),
    refetch: () => Promise.all(reads.map((read) => read.refetch())),
    data: required.map((server, index) => ({
      server,
      candidates: candidatesQueries[index]?.data ?? [],
      bindings: bindingsQueries[index]?.data?.items ?? [],
      clients: clientsQueries[index]?.data ?? [],
    })),
  };
  const refetchAccounts = useRef(query.refetch);
  refetchAccounts.current = query.refetch;
  // A row earns a card when it asks for an account, or when it still holds a
  // binding whose authorization is gone: that binding blocks `ready`, and
  // disconnecting it is the only way past, so the row has to stay on screen.
  const shows = (row: (typeof query.data)[number]) =>
    row.clients.length > 0 ||
    row.bindings.some((binding) => binding.remoteSession === undefined);
  const ready =
    query.isSuccess &&
    !query.isFetching &&
    !error &&
    !pending &&
    query.data.every(
      (row) =>
        row.bindings.every(
          (binding) =>
            binding.remoteSession !== undefined &&
            connectionLifetime(binding.remoteSession).kind !== "ended",
        ) &&
        row.clients.every((client) =>
          row.bindings.some(
            (binding) =>
              binding.remoteSession !== undefined &&
              binding.remoteSessionClientId === client.id &&
              connectionLifetime(binding.remoteSession).kind !== "ended",
          ),
        ),
    );
  useEffect(() => {
    onReady(ready);
  }, [ready, onReady]);
  useEffect(() => {
    if (
      !refreshing ||
      !canManage ||
      refreshInFlight.current ||
      pending ||
      error ||
      !query.isSuccess ||
      query.isFetching
    )
      return;
    const row = query.data.find(
      (row) => row.server.id === refreshing.server.id,
    );
    const renewed = row?.candidates.find(
      (candidate) =>
        candidate.remoteSessionClientId === refreshing.clientId &&
        connectionLifetime(candidate).kind !== "ended",
    );
    if (!renewed) return;
    refreshInFlight.current = true;
    setPending(true);
    const request = {
      gramProject: refreshing.server.projectSlug,
      principalId: agent.id,
      userSessionIssuerId: refreshing.server.issuerId!,
    };
    void (async () => {
      try {
        await sdk.remoteSessions.detachBinding({
          gramProject: request.gramProject,
          detachBindingRequestBody: {
            principalId: request.principalId,
            userSessionIssuerId: request.userSessionIssuerId,
            id: refreshing.bindingId,
          },
        });
        await sdk.remoteSessions.attachBinding({
          gramProject: request.gramProject,
          attachBindingRequestBody: {
            principalId: request.principalId,
            userSessionIssuerId: request.userSessionIssuerId,
            remoteSessionId: renewed.id,
          },
        });
        setRefreshing(undefined);
        await refetchAccounts.current();
      } catch {
        setError(true);
      } finally {
        refreshInFlight.current = false;
        setPending(false);
      }
    })();
  }, [
    refreshing,
    canManage,
    pending,
    error,
    query.isSuccess,
    query.isFetching,
    query.data,
    sdk,
    agent.id,
  ]);
  async function attach(server: KeyServer, sessionId: string) {
    setPending(true);
    setError(false);
    try {
      await attachBinding.mutateAsync({
        request: {
          gramProject: server.projectSlug,
          attachBindingRequestBody: {
            principalId: agent.id,
            userSessionIssuerId: server.issuerId!,
            remoteSessionId: sessionId,
          },
        },
      });
      await query.refetch();
    } catch {
      setError(true);
    } finally {
      setPending(false);
    }
  }
  async function detach(server: KeyServer, bindingId: string) {
    setPending(true);
    setError(false);
    try {
      await detachBinding.mutateAsync({
        request: {
          gramProject: server.projectSlug,
          detachBindingRequestBody: {
            principalId: agent.id,
            userSessionIssuerId: server.issuerId!,
            id: bindingId,
          },
        },
      });
      await query.refetch();
    } catch {
      setError(true);
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="space-y-4">
      {!compact && (
        <div>
          <h2 className="text-lg font-semibold">Connect accounts</h2>
          {/* Only the case in front of the user is described: the step says
            either what to connect, or that there is nothing to connect. */}
          <Text small muted>
            {/* An issuer-backed server still needs no account until it asks for
              one, so the sentence follows what the rows actually show. */}
            {query.data.every((row) => row.clients.length === 0)
              ? "None of the servers you chose need one. Continue."
              : "Choose a connected account, or connect a new one in a separate tab and refresh this list. Accounts belong to the agent, so they stay connected for its other keys."}
          </Text>
        </div>
      )}
      {query.isLoading && <Text>Checking required accounts…</Text>}
      {query.isError && (
        <Text role="alert">
          Could not verify account requirements. Refresh before continuing.
        </Text>
      )}
      {/* A server that asks for nothing gets no card of its own; the sentence
          above already covers it, and an empty card reads as unfinished. */}
      {query.data?.filter(shows).map((row) => (
        <section
          key={`${row.server.projectId}:${row.server.issuerId}`}
          className={
            compact ? "space-y-3" : "border-border space-y-4 border p-5"
          }
        >
          {!compact && (
            <div className="flex items-center gap-2">
              <Server
                aria-hidden="true"
                className="text-muted-foreground size-4"
              />
              <h3 className="text-sm font-medium">{row.server.name}</h3>
            </div>
          )}
          {!canManage && row.clients.length > 0 && (
            <Text small muted>
              You need permission to edit this agent to change its connections.
            </Text>
          )}
          {canManage &&
            row.server.connectUrl &&
            (!compact ||
              row.bindings.length === 0 ||
              row.bindings.some(
                (b) =>
                  b.remoteSession &&
                  connectionLifetime(b.remoteSession).kind === "ended",
              )) && (
              <a
                href={row.server.connectUrl}
                target="_blank"
                rel="noopener noreferrer"
                onClick={() => {
                  const expired = row.bindings.find(
                    (binding) =>
                      binding.remoteSession?.subjectUrn === `user:${user.id}` &&
                      connectionLifetime(binding.remoteSession).kind ===
                        "ended",
                  );
                  if (expired)
                    setRefreshing({
                      bindingId: expired.id,
                      clientId: expired.remoteSessionClientId,
                      server: row.server,
                    });
                }}
                className="text-primary inline-flex items-center gap-1.5 text-sm underline underline-offset-4"
              >
                {row.bindings.some(
                  (binding) =>
                    binding.remoteSession?.subjectUrn === `user:${user.id}` &&
                    connectionLifetime(binding.remoteSession).kind === "ended",
                )
                  ? "Refresh connection"
                  : "Connect an account"}
                <ExternalLink aria-hidden="true" className="size-3.5" />
              </a>
            )}
          {row.bindings
            .filter((binding) => binding.remoteSession)
            .map((binding) => {
              const session = binding.remoteSession!;
              const mine = session.subjectUrn === `user:${user.id}`;
              const locked = !mine;
              const label =
                session.subjectDisplayName ||
                session.subjectEmail ||
                "Another member";
              return (
                <div
                  key={binding.id}
                  className={cn(
                    "flex items-center gap-3 border p-3",
                    locked && "bg-muted/40 text-muted-foreground",
                  )}
                >
                  <Avatar title={label}>
                    <AvatarImage
                      src={
                        mine
                          ? user.photoUrl
                          : members?.members.find(
                              (member) =>
                                `user:${member.id}` === session.subjectUrn,
                            )?.photoUrl
                      }
                      alt={label}
                    />
                    <AvatarFallback>
                      {label.slice(0, 2).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <div className="min-w-0 flex-1">
                    <Text small>
                      {mine ? "Your connection" : `${label}'s connection`}
                    </Text>
                    <Text small muted>
                      {connectionLifetimeLabel(connectionLifetime(session))}
                    </Text>
                  </div>
                  {!canManage ? (
                    <LockKeyhole
                      className="size-4"
                      aria-label="Read-only connection"
                    />
                  ) : (
                    <Button
                      variant="tertiary"
                      size="sm"
                      disabled={pending || query.isFetching}
                      onClick={() => void detach(row.server, binding.id)}
                    >
                      Remove assignment
                    </Button>
                  )}
                </div>
              );
            })}
          {row.bindings
            .filter((binding) => !binding.remoteSession)
            .map((binding) => (
              <div
                key={binding.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3"
              >
                <Text small>
                  Account unavailable. Disconnect its old authorization before
                  choosing an account again.
                </Text>
                <Button
                  variant="secondary"
                  disabled={
                    !canManage ||
                    pending ||
                    error ||
                    query.isFetching ||
                    query.isError
                  }
                  onClick={() => void detach(row.server, binding.id)}
                >
                  Disconnect unavailable account
                </Button>
              </div>
            ))}
          {(canManage ? row.clients : []).map((client, clientIndex) => (
            <div key={client.id} className="space-y-2">
              {row.clients.length > 1 && (
                <Text small muted>
                  Connection {clientIndex + 1}
                </Text>
              )}
              {row.candidates
                .filter(
                  (c) =>
                    c.remoteSessionClientId === client.id &&
                    (!compact ||
                      !row.bindings.some(
                        (b) => b.remoteSessionId === c.id && b.remoteSession,
                      )),
                )
                .map((candidate) => {
                  const attached = row.bindings.some(
                    (b) =>
                      b.remoteSession !== undefined &&
                      b.remoteSessionId === candidate.id,
                  );
                  const occupied = row.bindings.some(
                    (b) => b.remoteSessionClientId === client.id,
                  );
                  return (
                    <div
                      key={candidate.id}
                      className="bg-muted/30 flex flex-wrap items-center justify-between gap-3 p-3"
                    >
                      <div className="min-w-0 flex-1 space-y-1">
                        <Text small className="font-medium">
                          {sessionAccountLabel(candidate)}
                        </Text>
                        <Text small muted className="break-words">
                          {candidate.scopes.length > 0
                            ? `Access: ${candidate.scopes.join(", ")}`
                            : "No additional access requested"}
                        </Text>
                        <Text small muted>
                          {connectionLifetimeLabel(
                            connectionLifetime(candidate),
                          )}
                        </Text>
                      </div>
                      <Button
                        disabled={
                          attached ||
                          occupied ||
                          connectionLifetime(candidate).kind === "ended" ||
                          pending ||
                          error ||
                          query.isFetching ||
                          query.isError
                        }
                        onClick={() => void attach(row.server, candidate.id)}
                      >
                        {attached
                          ? "Connected"
                          : occupied
                            ? "Another account in use"
                            : "Use account"}
                      </Button>
                    </div>
                  );
                })}
              {!row.candidates.some(
                (c) => c.remoteSessionClientId === client.id,
              ) && (
                <Text small muted>
                  No connected accounts yet. Connect an account, then refresh
                  this list.
                </Text>
              )}
            </div>
          ))}
        </section>
      ))}
      {error && (
        <Text role="alert">
          Could not confirm the account connection. Refresh to check its status
          before trying again.
        </Text>
      )}
      {/* Nothing to connect means nothing to refresh, confirm or switch: the
          step keeps only the sentence that says so. A failed read reports no
          clients at all, so it keeps the refresh the alert above asks for. */}
      {(query.isError || (query.data.some(shows) && (!compact || !ready))) && (
        <>
          <Button
            variant="secondary"
            disabled={pending}
            onClick={() => {
              setError(false);
              void query.refetch();
            }}
          >
            Refresh accounts
          </Button>
          {ready && (
            <div role="status" className="flex items-center gap-2 text-sm">
              <CheckCircle2 aria-hidden="true" className="size-4" />
              Accounts ready.
            </div>
          )}
          <Text small muted>
            You can assign only your own connections. Removing an assignment
            does not disconnect that person’s account.
          </Text>
        </>
      )}
    </div>
  );
}
