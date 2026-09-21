import { sessionAccountLabel } from "./session-account-identity";
import { useEffect, useRef, useState } from "react";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import { firstPartyConnectUrl } from "@/lib/utils";
import type { Toolset } from "@/lib/toolTypes";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  useIsPlatformAdmin,
  useOrganization,
  useProject,
  useSession,
} from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { Text } from "@/components/ui/Text";
import {
  AttachedUserSessionsPanel,
  type AttachableSession,
} from "./AttachedUserSessionsPanel";
import { collectPageItems } from "./collectPageItems";
import { queryKeyAgents } from "@gram/client/react-query/agents.js";
import { queryKeyRemoteSessions } from "@gram/client/react-query/remoteSessions.js";
import { queryKeyRemoteSessionsListBindings } from "@gram/client/react-query/remoteSessionsListBindings.js";
import { useRemoteSessionsAttachBindingMutation } from "@gram/client/react-query/remoteSessionsAttachBinding.js";
import { useRemoteSessionsDetachBindingMutation } from "@gram/client/react-query/remoteSessionsDetachBinding.js";

export function AttachedUserSessions({
  issuerId,
  connectUrl,
}: {
  issuerId: string;
  connectUrl?: string;
}): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const scopeOverride = getRBACScopeOverrideHeader(
    import.meta.env.DEV || isPlatformAdmin,
  );
  const blocked =
    session.organizationOverride ||
    Boolean(session.impersonatorEmail) ||
    scopeOverride !== null;
  if (organization.slug === DEMO_ORG_SLUG)
    return (
      <Text small muted>
        Upstream account attachments require active organization membership and
        are unavailable in the shared demo.
      </Text>
    );
  if (blocked)
    return (
      <Text small muted>
        Use an ordinary Gram session without an RBAC scope override to manage
        upstream account attachments.
      </Text>
    );
  return (
    <OwnedSessionBindings
      key={`${organization.id}:${project.id}:${session.user.id}:${issuerId}`}
      issuerId={issuerId}
      connectUrl={connectUrl}
    />
  );
}

function OwnedSessionBindings({
  issuerId,
  connectUrl,
}: {
  issuerId: string;
  connectUrl?: string;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const project = useProject();
  const session = useSession();
  const attachBinding = useRemoteSessionsAttachBindingMutation();
  const detachBinding = useRemoteSessionsDetachBindingMutation();
  const lifetime = useRef<AbortController | null>(null);
  const saving = useRef(false);
  const [saveError, setSaveError] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, []);
  const ownerScope = {
    organizationId: organization.id,
    userId: session.user.id,
  };
  const agentsQuery = useQuery({
    queryKey: [...queryKeyAgents({}), ownerScope],
    queryFn: ({ signal }) => sdk.agents.list(undefined, undefined, { signal }),
    retry: false,
    throwOnError: false,
  });
  const agents = (agentsQuery.data ?? []).filter(
    (agent) =>
      agent.permissions.authorize && agent.ownerUserId === session.user.id,
  );
  const requests = agents.map((agent) => ({
    gramProject: project.slug,
    principalId: agent.id,
    userSessionIssuerId: issuerId,
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
  const reads = [agentsQuery, ...candidatesQueries, ...bindingsQueries];
  const query = {
    isLoading: reads.some((read) => read.isLoading),
    isFetching: reads.some((read) => read.isFetching),
    isError: reads.some((read) => read.isError),
    refetch: () => Promise.all(reads.map((read) => read.refetch())),
  };
  const rows = agents.map((agent, index) => ({
    agent,
    candidates: candidatesQueries[index]?.data ?? [],
    bindings: bindingsQueries[index]?.data?.items ?? [],
  }));
  const sessions = new Map<string, AttachableSession>();
  for (const { candidates, bindings } of rows) {
    for (const candidate of candidates) {
      sessions.set(candidate.id, {
        id: candidate.id,
        label: sessionAccountLabel(candidate),
        status: "active",
        agentIds: [],
      });
    }
    for (const binding of bindings) {
      if (!sessions.has(binding.remoteSessionId))
        sessions.set(binding.remoteSessionId, {
          id: binding.remoteSessionId,
          label: binding.remoteSession
            ? sessionAccountLabel(binding.remoteSession)
            : "Account unavailable",
          status: "unavailable",
          agentIds: [],
        });
    }
  }
  // An unchanged session ID can now represent a different OAuth grant. Do not
  // merge a tombstone attachment into that newly eligible grant's active state.
  // Keep it detach-only until all stale authorizations have been removed.
  for (const { bindings } of rows) {
    for (const binding of bindings) {
      if (!binding.remoteSession) {
        sessions.set(binding.remoteSessionId, {
          id: binding.remoteSessionId,
          label: "Account authorization unavailable",
          status: "unavailable",
          agentIds: [],
        });
      }
    }
  }
  for (const { agent, bindings } of rows)
    for (const binding of bindings) {
      const item = sessions.get(binding.remoteSessionId);
      if (item && !item.agentIds.includes(agent.id))
        item.agentIds.push(agent.id);
    }
  return (
    <>
      {!query.isLoading && !query.isError && rows.length === 0 && (
        <Text small muted>
          Create an agent you own to attach upstream sessions.
        </Text>
      )}
      {saveError && (
        <Text role="alert">
          Could not save all attachments. Some changes may have succeeded.
          Refresh accounts to check current access before trying again.
        </Text>
      )}
      <AttachedUserSessionsPanel
        sessions={[...sessions.values()]}
        emptyMessage={
          rows.length === 0
            ? "Accounts cannot be listed until an eligible agent is available."
            : undefined
        }
        agents={rows.map(({ agent }) => ({
          id: agent.id,
          name: agent.name,
          eligible:
            agent.lifecycle === "active" && !agent.ownerReassignmentRequiredAt,
        }))}
        isLoading={query.isFetching || isSaving}
        isError={query.isError}
        connectUrl={connectUrl}
        onRefresh={() => {
          setSaveError(false);
          void query.refetch();
        }}
        eligibleAgentIdsBySession={Object.fromEntries(
          [...sessions.keys()].map((id) => [
            id,
            rows
              .filter(({ candidates }) =>
                candidates.some((candidate) => candidate.id === id),
              )
              .map(({ agent }) => agent.id),
          ]),
        )}
        onSave={async (sessionId, selected) => {
          const signal = lifetime.current?.signal;
          if (
            !signal ||
            signal.aborted ||
            saving.current ||
            query.isFetching ||
            query.isError ||
            saveError
          )
            throw new Error("Attachments are unavailable");
          if (selected.some((id) => !rows.some(({ agent }) => agent.id === id)))
            throw new Error("Agent is no longer eligible");
          for (const { agent, candidates, bindings } of rows) {
            if (
              selected.includes(agent.id) &&
              !bindings.some(
                (binding) => binding.remoteSessionId === sessionId,
              ) &&
              (agent.lifecycle !== "active" ||
                agent.ownerReassignmentRequiredAt ||
                !candidates.some((candidate) => candidate.id === sessionId))
            )
              throw new Error("Agent is no longer eligible");
          }
          const operations = rows.flatMap<() => Promise<unknown>>(
            ({ agent, bindings }) => {
              const current = bindings.filter(
                (binding) => binding.remoteSessionId === sessionId,
              );
              if (!selected.includes(agent.id))
                return current.map(
                  (binding) => () =>
                    detachBinding.mutateAsync({
                      request: {
                        gramProject: project.slug,
                        detachBindingRequestBody: {
                          principalId: agent.id,
                          userSessionIssuerId: issuerId,
                          id: binding.id,
                        },
                      },
                      options: { signal },
                    }),
                );
              if (current.length > 0) return [];
              return [
                () =>
                  attachBinding.mutateAsync({
                    request: {
                      gramProject: project.slug,
                      attachBindingRequestBody: {
                        principalId: agent.id,
                        userSessionIssuerId: issuerId,
                        remoteSessionId: sessionId,
                      },
                    },
                    options: { signal },
                  }),
              ];
            },
          );
          // Validate and plan everything before starting any effect. Wait for all
          // writes, including failures, before allowing another save or refresh.
          saving.current = true;
          setIsSaving(true);
          setSaveError(false);
          try {
            const results = await Promise.allSettled(
              operations.map((operation) =>
                Promise.resolve().then(() => {
                  signal.throwIfAborted();
                  return operation();
                }),
              ),
            );
            if (signal.aborted) return;
            if (results.some((result) => result.status === "rejected")) {
              setSaveError(true);
              throw new Error("Some attachments could not be saved");
            }
            await query.refetch();
          } finally {
            saving.current = false;
            if (!signal.aborted) setIsSaving(false);
          }
        }}
      />
    </>
  );
}

export function ToolsetAttachedUserSessions({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element | null {
  const url = useInternalMcpUrl(toolset);
  return toolset.userSessionIssuerId ? (
    <AttachedUserSessions
      issuerId={toolset.userSessionIssuerId}
      connectUrl={firstPartyConnectUrl(url, { runtimePath: "mcp" })}
    />
  ) : null;
}
