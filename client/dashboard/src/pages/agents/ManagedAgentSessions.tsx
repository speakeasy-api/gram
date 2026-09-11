import {
  hashKey,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentSessionsSection } from "./AgentSessions";

export function ManagedAgentSessions({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  const queryClient = useQueryClient();
  const queryKey = ["managed-agent-sessions", organization.id, agent.id];
  // Both list and revoke require the server's ownership-or-authorize decision.
  // agent:read alone does not grant access to credentials.
  const canManage = agent.permissions.authorize;
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
      sessions={sessions.data?.pages.flatMap((page) => page.items) ?? []}
      isLoading={sessions.isLoading}
      isError={sessions.isError && !sessions.isFetchNextPageError}
      canRead={canManage}
      canRevoke={canManage}
      onRetry={() => void sessions.refetch()}
      hasMore={sessions.hasNextPage}
      isLoadingMore={sessions.isFetchingNextPage}
      loadMoreError={sessions.isFetchNextPageError}
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
