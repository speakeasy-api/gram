import {
  hashKey,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { AgentSessionsSection } from "./AgentSessions";

export function ManagedAgentSessions({
  agent,
  variant = "section",
  limit,
}: {
  agent: ManagedAgent;
  /** "bare" drops the section chrome for a caller that supplies its own. */
  variant?: "section" | "bare";
  /**
   * Show at most this many sessions and drop the pager. For a summary on
   * another page, whose "Show all" goes to the section that owns them.
   */
  limit?: number;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const queryKey = ["managed-agent-sessions", organization.id, agent.id];
  // Both list and revoke require the server's ownership-or-authorize decision.
  // agent:read alone does not grant access to credentials.
  const canManage = agent.permissions.authorize;
  // A session carries its authorizer's id and nothing else, so the name
  // comes from the member list. A failed read leaves the id, which still
  // identifies them and still links.
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const sessions = useInfiniteQuery({
    queryKey,
    queryKeyHashFn: hashKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam, signal }) => {
      const page = await sdk.agents.listSessions(
        { agentId: agent.id, cursor: pageParam, limit: 50 },
        undefined,
        { signal },
      );
      return page.result;
    },
    getNextPageParam: (page) => page.nextCursor || undefined,
    enabled: canManage,
    throwOnError: false,
    retry: false,
  });

  return (
    <AgentSessionsSection
      key={`${organization.id}-${agent.id}`}
      variant={variant}
      sessions={(() => {
        const all = sessions.data?.pages.flatMap((page) => page.items) ?? [];
        return limit === undefined ? all : all.slice(0, limit);
      })()}
      isLoading={sessions.isLoading}
      isError={sessions.isError && !sessions.isFetchNextPageError}
      canRead={canManage}
      canRevoke={canManage}
      onRetry={() => void sessions.refetch()}
      authorizerName={(userId) =>
        members.data?.members.find((member) => member.id === userId)?.name
      }
      // A capped list pages nowhere: the link out is the way to see more.
      hasMore={limit === undefined && sessions.hasNextPage}
      isLoadingMore={limit === undefined && sessions.isFetchingNextPage}
      loadMoreError={limit === undefined && sessions.isFetchNextPageError}
      onLoadMore={() => void sessions.fetchNextPage()}
      onRevoke={async (session) => {
        if (!canManage) throw new Error("Agent credential permission required");
        await sdk.agents.revokeSession({
          revokeSessionRequestBody: {
            agentId: agent.id,
            sessionId: session.id,
          },
        });
        await queryClient.invalidateQueries({ queryKey });
      }}
    />
  );
}
