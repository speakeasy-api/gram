import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useMembers } from "@gram/client/react-query/members.js";
import { AgentSessionsSection, type AgentSessionRow } from "./AgentSessions";
import { useAgentSessionList } from "./useAgentSessionList";

const security = { sessionHeaderGramSession: "" };

export function ManagedAgentSessions({
  agent,
  variant = "section",
  limit,
  credentialsHref,
}: {
  agent: ManagedAgent;
  /** Where the agent's keys and federated access are managed. */
  credentialsHref?: string;
  /**
   * "bare" drops the section chrome for a caller that supplies its own.
   * "panel" also drops the table's frame, inside a bordered panel.
   */
  variant?: "section" | "bare" | "panel";
  /**
   * Show at most this many sessions and drop the pager. For a summary on
   * another page, whose "Show all" goes to the section that owns them.
   */
  limit?: number;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const sessions = useAgentSessionList(agent);
  const canManage = sessions.canManage;
  // A session carries its authorizer's id and nothing else, so the name
  // comes from the member list. A failed read leaves the id, which still
  // identifies them and still links. Not read at all by someone who may not
  // see the sessions: the org roster is a separate, often-refused request.
  const members = useMembers(undefined, undefined, {
    enabled: canManage,
    throwOnError: false,
  });
  // A key authenticates each request on its own and opens no session, so an
  // agent that only ever uses its key would otherwise look unconnected. Same
  // options as the credentials panel, so both read one cached list.
  const keys = useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organization.id, key]),
    enabled: canManage,
    retry: false,
    throwOnError: false,
  });
  // A key that was never used is a credential, not a connection: it stays on
  // the credentials tab only.
  const keyRows = useMemo<AgentSessionRow[]>(
    () =>
      (keys.data?.keys ?? [])
        .filter((key) => key.lastAccessedAt !== undefined)
        .sort(
          (a, b) =>
            (b.lastAccessedAt?.getTime() ?? 0) -
            (a.lastAccessedAt?.getTime() ?? 0),
        )
        .map((key) => ({
          id: key.id,
          subjectKind: "api-key",
          clientName: key.name,
          // Issuing the key is what let the agent connect with it.
          authorizerUserId: key.createdByUserId || undefined,
          keyPrefix: key.keyPrefix,
          createdAt: key.createdAt,
          expiresAt: key.expiresAt ?? key.createdAt,
          refreshExpiresAt: key.expiresAt,
          lastUsedAt: key.lastAccessedAt,
        })),
    [keys.data],
  );
  // Memoized: the table's expiry timer re-subscribes whenever its rows change.
  const allSessions = sessions.sessions;
  const rows = useMemo(() => {
    const merged = [...keyRows, ...allSessions];
    return limit === undefined ? merged : merged.slice(0, limit);
  }, [keyRows, allSessions, limit]);
  // Keys and sessions are read apart, so rows from either one are enough to
  // show. Loading and failure only take over the table when there is nothing
  // to show, and then a failure in either read is reported, not mistaken for
  // "no connections".
  const sessionsFailed = sessions.isError && !sessions.isFetchNextPageError;
  const isLoading = rows.length === 0 && (sessions.isLoading || keys.isLoading);
  const isError =
    rows.length === 0 && !isLoading && (sessionsFailed || keys.isError);

  return (
    <AgentSessionsSection
      key={`${organization.id}-${agent.id}`}
      variant={variant}
      sessions={rows}
      isLoading={isLoading}
      isError={isError}
      canRead={canManage}
      canRevoke={canManage}
      onRetry={() => {
        sessions.refetch();
        void keys.refetch();
      }}
      credentialsHref={credentialsHref}
      authorizerName={(userId) =>
        members.data?.members.find((member) => member.id === userId)?.name
      }
      // A capped list pages nowhere: the link out is the way to see more.
      hasMore={limit === undefined && sessions.hasNextPage}
      isLoadingMore={limit === undefined && sessions.isFetchingNextPage}
      loadMoreError={limit === undefined && sessions.isFetchNextPageError}
      onLoadMore={sessions.fetchNextPage}
      onRevoke={async (session) => {
        if (!canManage) throw new Error("Agent credential permission required");
        await sdk.agents.revokeSession({
          revokeSessionRequestBody: {
            agentId: agent.id,
            sessionId: session.id,
          },
        });
        await queryClient.invalidateQueries({ queryKey: sessions.queryKey });
      }}
    />
  );
}
