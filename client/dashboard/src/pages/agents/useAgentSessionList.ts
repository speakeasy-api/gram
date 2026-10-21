import { hashKey, useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { AgentSessionRow } from "./AgentSessions";

/**
 * One agent's sessions, read once for every panel that needs them.
 *
 * The sessions table and the overview's "Acting for" ranking both read this
 * list, and under query keys of their own each paid for the same request.
 *
 * `all` follows the cursor to exhaustion. A ranking taken from the first page
 * is not a ranking: an agent with more sessions than one page holds would be
 * counted from an arbitrary slice of them.
 */
export function useAgentSessionList(
  agent: ManagedAgent,
  { all = false }: { all?: boolean } = {},
): {
  sessions: AgentSessionRow[];
  /** The key the list is cached under, for invalidating it after a revoke. */
  queryKey: unknown[];
  /** Whether this caller may read the list at all. */
  canManage: boolean;
  isPending: boolean;
  isLoading: boolean;
  isError: boolean;
  isFetchNextPageError: boolean;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => void;
  refetch: () => void;
} {
  const sdk = useSdkClient();
  const organization = useOrganization();
  // Both list and revoke require the server's ownership-or-authorize decision.
  // agent:read alone does not grant access to credentials.
  const canManage = agent.permissions.authorize;
  const queryKey = ["managed-agent-sessions", organization.id, agent.id];
  const query = useInfiniteQuery({
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

  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = query;
  useEffect(() => {
    // A failed page stops the walk rather than retrying it forever; the
    // caller's retry starts it again.
    if (!all || !hasNextPage || isFetchingNextPage || isFetchNextPageError) {
      return;
    }
    void fetchNextPage();
  }, [
    all,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  const pages = query.data?.pages;
  const sessions = useMemo(
    () => pages?.flatMap((page) => page.items) ?? [],
    [pages],
  );

  return {
    sessions,
    queryKey,
    canManage,
    isPending: query.isPending,
    isLoading: query.isLoading,
    isError: query.isError,
    isFetchNextPageError,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage: () => void fetchNextPage(),
    refetch: () => void query.refetch(),
  };
}
