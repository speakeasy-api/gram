import { useProject } from "@/contexts/Auth";
import {
  keepPreviousData,
  useInfiniteQuery,
  useQuery,
  type InfiniteData,
  type UseInfiniteQueryResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { fixtureConnectionHealthSource, fixtureNow } from "./fixtures";
import type { ConnectionHealthSource } from "./source";
import type {
  ActiveKillswitch,
  AttemptCountsResponse,
  AttemptFilters,
  DeniedClientsResponse,
  GetAttemptResponse,
  GetSuspectedProblemResponse,
  ListAttemptsResponse,
  ListSuspectedProblemsResponse,
  SessionActivityResponse,
  SignInSummaryResponse,
  SummaryBucket,
} from "./types";

// The seam: point these at the SDK and `() => new Date()` when the API ships.
const source: ConnectionHealthSource = fixtureConnectionHealthSource;
export const connectionHealthNow: () => Date = fixtureNow;

/** A resolved window, as every range read takes it. */
export interface Window {
  from: string;
  to: string;
}

const ROOT = "mcpConnectionHealth";

function useKey(...parts: unknown[]): unknown[] {
  const project = useProject();
  return [ROOT, project.id, ...parts];
}

interface Scope {
  providerId: string;
  serverId?: string;
}

export function useConnectionAttempts(
  scope: Scope,
  window: Window,
  filters: AttemptFilters,
  options: { limit?: number; enabled?: boolean } = {},
): UseInfiniteQueryResult<InfiniteData<ListAttemptsResponse>> {
  const queryKey = useKey(
    "listAttempts",
    scope,
    window,
    filters,
    options.limit,
  );
  return useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) =>
      source.listAttempts({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
        filters,
        cursor: pageParam,
        limit: options.limit,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor,
    placeholderData: keepPreviousData,
    enabled: options.enabled ?? true,
  });
}

export function useAttemptCounts(
  scope: Scope,
  window: Window,
  filters: AttemptFilters,
  options: { enabled?: boolean } = {},
): UseQueryResult<AttemptCountsResponse> {
  const queryKey = useKey("getAttemptCounts", scope, window, filters);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.getAttemptCounts({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
        filters,
      }),
    placeholderData: keepPreviousData,
    enabled: options.enabled ?? true,
  });
}

export function useAttempt(
  reference: string | null,
): UseQueryResult<GetAttemptResponse> {
  const queryKey = useKey("getAttempt", reference);
  return useQuery({
    queryKey,
    queryFn: () => source.getAttempt({ reference: reference ?? "" }),
    enabled: !!reference,
  });
}

export function useSignInSummary(
  scope: Scope,
  window: Window,
  bucket?: SummaryBucket,
): UseQueryResult<SignInSummaryResponse> {
  const queryKey = useKey("getSignInSummary", scope, window, bucket);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.getSignInSummary({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
        bucket,
      }),
    placeholderData: keepPreviousData,
  });
}

export function useSessionActivity(
  providerId: string,
  window: Window,
  options: { enabled?: boolean } = {},
): UseQueryResult<SessionActivityResponse> {
  const queryKey = useKey("getSessionActivity", providerId, window);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.getSessionActivity({ userSessionIssuerId: providerId, ...window }),
    placeholderData: keepPreviousData,
    enabled: options.enabled ?? true,
  });
}

export function useDeniedClients(
  scope: Scope,
  window: Window,
): UseQueryResult<DeniedClientsResponse> {
  const queryKey = useKey("listDeniedClients", scope, window);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.listDeniedClients({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
      }),
    placeholderData: keepPreviousData,
  });
}

export function useSuspectedProblems(
  scope: Scope,
): UseQueryResult<ListSuspectedProblemsResponse> {
  const queryKey = useKey("listSuspectedProblems", scope);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.listSuspectedProblems({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
      }),
  });
}

export function useSuspectedProblem(
  id: string | null,
): UseQueryResult<GetSuspectedProblemResponse> {
  const queryKey = useKey("getSuspectedProblem", id);
  return useQuery({
    queryKey,
    queryFn: () => source.getSuspectedProblem({ id: id ?? "" }),
    enabled: !!id,
  });
}

export function useActiveKillswitches(
  providerId: string,
): UseQueryResult<ActiveKillswitch[]> {
  const queryKey = useKey("killswitches", providerId);
  return useQuery({
    queryKey,
    queryFn: () =>
      source.listActiveKillswitches({ userSessionIssuerId: providerId }),
  });
}
