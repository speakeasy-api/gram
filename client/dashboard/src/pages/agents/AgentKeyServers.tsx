import { sessionAccountLabel } from "@/components/sessions/session-account-identity";
import { useEffect, useState, type JSX } from "react";
import { useQueries } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Server, CheckCircle2, ExternalLink } from "lucide-react";
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
            ? firstPartyConnectUrl(`${getServerURL()}/mcp/${detail.mcpSlug}`, {
                runtimePath: "mcp",
              })
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
                    This server doesn’t accept Gram API keys.
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

function ServerAccounts({
  agent,
  servers,
  onReady,
}: {
  agent: ManagedAgent;
  servers: KeyServer[];
  onReady: (ready: boolean) => void;
}) {
  const organization = useOrganization();
  const { user } = useSession();
  const sdk = useSdkClient();
  const required = [
    ...new Map(
      servers
        .filter((s) => s.issuerId)
        .map((s) => [`${s.projectId}:${s.issuerId}`, s]),
    ).values(),
  ];
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const attachBinding = useRemoteSessionsAttachBindingMutation();
  const detachBinding = useRemoteSessionsDetachBindingMutation();
  const ownerScope = { organizationId: organization.id, userId: user.id };
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
  const ready =
    query.isSuccess &&
    !query.isFetching &&
    !error &&
    !pending &&
    query.data.every(
      (row) =>
        row.bindings.every((binding) => binding.remoteSession !== undefined) &&
        row.clients.every((client) =>
          row.bindings.some(
            (binding) =>
              binding.remoteSession !== undefined &&
              binding.remoteSessionClientId === client.id &&
              row.candidates.some(
                (candidate) => candidate.id === binding.remoteSessionId,
              ),
          ),
        ),
    );
  useEffect(() => {
    onReady(ready);
  }, [ready, onReady]);
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
      <Text>
        Choose a connected account, or connect a new one in a separate tab and
        refresh this list.
      </Text>
      {required.length > 0 && (
        <Text small muted>
          Accounts are connected to the agent, not just this key. They stay
          connected if you cancel and can be used by its other keys.
        </Text>
      )}
      {query.isLoading && <Text>Checking required accounts…</Text>}
      {query.isError && (
        <Text role="alert">
          Could not verify account requirements. Refresh before continuing.
        </Text>
      )}
      {required.length === 0 && (
        <Text>No account connection needed for these servers.</Text>
      )}
      {query.data?.map((row) => (
        <section
          key={`${row.server.projectId}:${row.server.issuerId}`}
          className="border-border space-y-4 border p-5"
        >
          <div className="flex items-center gap-2">
            <Server
              aria-hidden="true"
              className="text-muted-foreground size-4"
            />
            <h3 className="text-sm font-medium">{row.server.name}</h3>
          </div>
          {row.server.connectUrl && (
            <a
              href={row.server.connectUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary inline-flex items-center gap-1.5 text-sm font-medium underline underline-offset-4"
            >
              Connect an account
              <ExternalLink aria-hidden="true" className="size-3.5" />
            </a>
          )}
          {row.clients.length === 0 && (
            <Text small muted>
              No account connection needed.
            </Text>
          )}
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
                    pending || error || query.isFetching || query.isError
                  }
                  onClick={() => void detach(row.server, binding.id)}
                >
                  Disconnect unavailable account
                </Button>
              </div>
            ))}
          {row.clients.map((client, clientIndex) => (
            <div key={client.id} className="space-y-2">
              {row.clients.length > 1 && (
                <Text small muted>
                  Connection {clientIndex + 1}
                </Text>
              )}
              {row.candidates
                .filter((c) => c.remoteSessionClientId === client.id)
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
                      </div>
                      <Button
                        disabled={
                          attached ||
                          occupied ||
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
      {required.length > 0 && (
        <Text small muted>
          To switch accounts, disconnect the current account from the server’s
          Sessions page first. You’ll choose this key’s access in the next step.
        </Text>
      )}
    </div>
  );
}
