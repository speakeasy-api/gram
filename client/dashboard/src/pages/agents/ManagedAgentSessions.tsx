import { useQueryClient } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { AgentSessionsSection } from "./AgentSessions";
import { useAgentSessionList } from "./useAgentSessionList";

export function ManagedAgentSessions({
  agent,
  variant = "section",
  limit,
}: {
  agent: ManagedAgent;
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

  return (
    <AgentSessionsSection
      key={`${organization.id}-${agent.id}`}
      variant={variant}
      sessions={
        limit === undefined
          ? sessions.sessions
          : sessions.sessions.slice(0, limit)
      }
      isLoading={sessions.isLoading}
      isError={sessions.isError && !sessions.isFetchNextPageError}
      canRead={canManage}
      canRevoke={canManage}
      onRetry={sessions.refetch}
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
