import { useProject, useSession } from "@/contexts/Auth";
import { isExpectedAPIError } from "@/lib/errors";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useKillswitches } from "@gram/client/react-query/killswitches.js";
import {
  keepPreviousData,
  useInfiniteQuery,
  useQuery,
  type InfiniteData,
  type UseInfiniteQueryResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useState } from "react";
import { presetWindow } from "./filters";
import {
  fixtureGetSuspectedProblem,
  fixtureListSuspectedProblems,
} from "./problem-fixtures";
import * as sdk from "./sdk";
import type {
  ActiveKillswitch,
  AttemptCountsResponse,
  AttemptDetail,
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

export const connectionHealthNow = (): Date => new Date();

/** A resolved window, as every range read takes it. */
export interface Window {
  from: string;
  to: string;
}

interface Scope {
  providerId: string;
  serverId?: string;
}

export const CONNECTION_HEALTH_QUERY_ROOT = "mcpConnectionHealth";
const ROOT = CONNECTION_HEALTH_QUERY_ROOT;

function useClient() {
  const client = useGramContext();
  const project = useProject();
  return { client, gramProject: project.slug, projectId: project.id };
}

// A 4xx (flag off, bad range) will not change on retry.
const retry = (failures: number, error: Error) =>
  failures < 2 && !isExpectedAPIError(error);

export function useConnectionAttempts(
  scope: Scope,
  window: Window,
  filters: AttemptFilters,
  options: { limit?: number; enabled?: boolean } = {},
): UseInfiniteQueryResult<InfiniteData<ListAttemptsResponse>> {
  const { client, gramProject, projectId } = useClient();
  return useInfiniteQuery({
    queryKey: [
      ROOT,
      projectId,
      "listAttempts",
      scope,
      window,
      filters,
      options.limit,
    ],
    queryFn: ({ pageParam }) =>
      sdk.listAttempts(client, gramProject, {
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
    retry,
    throwOnError: false,
  });
}

export function useAttemptCounts(
  scope: Scope,
  window: Window,
  filters: AttemptFilters,
  options: { enabled?: boolean } = {},
): UseQueryResult<AttemptCountsResponse> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "getAttemptCounts", scope, window, filters],
    queryFn: () =>
      sdk.getAttemptCounts(client, gramProject, {
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
        filters,
      }),
    placeholderData: keepPreviousData,
    enabled: options.enabled ?? true,
    retry,
    throwOnError: false,
  });
}

function useAttempt(
  reference: string | null,
): UseQueryResult<GetAttemptResponse> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "getAttempt", reference],
    queryFn: () => sdk.getAttempt(client, gramProject, reference ?? ""),
    enabled: !!reference,
    retry,
    throwOnError: false,
  });
}

export function useSignInSummary(
  scope: Scope,
  window: Window,
  bucket?: SummaryBucket,
): UseQueryResult<SignInSummaryResponse> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "getSignInSummary", scope, window, bucket],
    queryFn: () =>
      sdk.getSignInSummary(client, gramProject, {
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
        bucket,
      }),
    placeholderData: keepPreviousData,
    retry,
    throwOnError: false,
  });
}

export function useSessionActivity(
  providerId: string,
  window: Window,
  options: { enabled?: boolean } = {},
): UseQueryResult<SessionActivityResponse> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "getSessionActivity", providerId, window],
    queryFn: () =>
      sdk.getSessionActivity(client, gramProject, {
        userSessionIssuerId: providerId,
        ...window,
      }),
    placeholderData: keepPreviousData,
    enabled: options.enabled ?? true,
    retry,
    throwOnError: false,
  });
}

/** The 30-day average session length; the API ignores the range for it. */
export function useSessionAverage(
  providerId: string,
): UseQueryResult<number | undefined> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "sessionAverage", providerId],
    queryFn: async () => {
      const result = await sdk.getSessionActivity(client, gramProject, {
        userSessionIssuerId: providerId,
        ...presetWindow("1d", connectionHealthNow()),
      });
      return result.averageSessionSeconds ?? null;
    },
    select: (value) => value ?? undefined,
    staleTime: 5 * 60 * 1000,
    retry,
    throwOnError: false,
  });
}

export function useDeniedClients(
  scope: Scope,
  window: Window,
): UseQueryResult<DeniedClientsResponse> {
  const { client, gramProject, projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "listDeniedClients", scope, window],
    queryFn: () =>
      sdk.listDeniedClients(client, gramProject, {
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
        ...window,
      }),
    placeholderData: keepPreviousData,
    retry,
    throwOnError: false,
  });
}

/**
 * Whether a looked-up attempt belongs to this provider (and locked server).
 * An attempt with no provider of its own belongs to the servers it reached.
 */
function useAttemptBelongs(
  attempt: AttemptDetail | undefined,
  providerId: string,
  lockedServerId?: string,
): "yes" | "no" | "pending" | "error" {
  const needsServers =
    !!attempt && !attempt.userSessionIssuerId && !!attempt.server;
  const [window] = useState(() => presetWindow("30d", connectionHealthNow()));
  const counts = useAttemptCounts({ providerId }, window, NO_FILTERS, {
    enabled: needsServers,
  });
  if (!attempt) return "pending";
  if (lockedServerId && attempt.server?.id !== lockedServerId) return "no";
  if (attempt.userSessionIssuerId)
    return attempt.userSessionIssuerId === providerId ? "yes" : "no";
  if (!attempt.server) return "yes";
  if (counts.isError) return "error";
  const servers = counts.data?.facets?.servers;
  if (!servers) return "pending";
  return servers.some((s) => s.value === attempt.server?.id) ? "yes" : "no";
}

/** An attempt looked up project-wide, kept only if it belongs here. */
export function useAttemptHere(
  reference: string | null,
  providerId: string,
  lockedServerId?: string,
): {
  query: UseQueryResult<GetAttemptResponse>;
  attempt: AttemptDetail | undefined;
  elsewhere: boolean;
  failed: boolean;
  isPending: boolean;
} {
  const query = useAttempt(reference);
  const found = query.data?.attempt;
  const belongs = useAttemptBelongs(found, providerId, lockedServerId);
  return {
    query,
    attempt: belongs === "yes" ? found : undefined,
    elsewhere: !!found && belongs === "no",
    failed: query.isError || belongs === "error",
    isPending: query.isPending || (!!found && belongs === "pending"),
  };
}

const NO_FILTERS: AttemptFilters = {};

/** Suspected problems are made-up sample data until their API ships. */
export const SUSPECTED_PROBLEMS_ARE_SAMPLE = true;

/** FIXTURES: suspected problems have no detector or API yet. */
export function useSuspectedProblems(
  scope: Scope,
): UseQueryResult<ListSuspectedProblemsResponse> {
  const { projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "listSuspectedProblems", scope],
    queryFn: () =>
      fixtureListSuspectedProblems({
        userSessionIssuerId: scope.providerId,
        serverId: scope.serverId,
      }),
    throwOnError: false,
  });
}

/** FIXTURES: see useSuspectedProblems. */
export function useSuspectedProblem(
  id: string | null,
): UseQueryResult<GetSuspectedProblemResponse> {
  const { projectId } = useClient();
  return useQuery({
    queryKey: [ROOT, projectId, "getSuspectedProblem", id],
    queryFn: () => fixtureGetSuspectedProblem({ id: id ?? "" }),
    enabled: !!id,
    throwOnError: false,
  });
}

/** The organization's killswitches in force now (first page of 100). */
export function useActiveKillswitches(): {
  killswitches: ActiveKillswitch[];
  hasMore: boolean;
  isPending: boolean;
  isError: boolean;
} {
  const session = useSession();
  const query = useKillswitches(
    { sessionHeaderGramSession: session.session },
    { status: "active", limit: 100, gramSession: session.session },
    { throwOnError: false },
  );
  const killswitches = (query.data?.result.items ?? []).map(
    (k): ActiveKillswitch => ({
      id: k.id,
      userId: k.userId,
      scope:
        k.scope.type === "all_servers"
          ? { type: "all_servers" }
          : { type: "selected_servers", serverIds: k.scope.serverIds },
      until:
        k.schedule.end === "bounded" ? k.schedule.endsAt.toISOString() : null,
    }),
  );
  // A member who may not read killswitches sees none rather than an error.
  const forbidden =
    typeof query.error === "object" &&
    query.error !== null &&
    "statusCode" in query.error &&
    (query.error.statusCode === 401 || query.error.statusCode === 403);
  return {
    killswitches,
    hasMore: !!query.data?.result.nextCursor,
    isPending: query.isPending,
    isError: query.isError && !forbidden,
  };
}
