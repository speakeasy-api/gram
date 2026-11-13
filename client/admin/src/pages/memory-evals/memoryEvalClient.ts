// Reads and writes for Organizational Memory eval runs, through the generated
// admin SDK.
//
// Every read and invalidation for this page goes through here, so a cache key
// is written once.

import {
  infiniteQueryOptions,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import type { AdminMemoryEvalRun } from "@gram/admin-client/models/components/adminmemoryevalrun";
import type { AdminMemoryEvalRunConfig } from "@gram/admin-client/models/components/adminmemoryevalrunconfig";
import {
  buildAdminGetMemoryEvalOptionsQuery,
  queryKeyAdminGetMemoryEvalOptions,
} from "@gram/admin-client/react-query/adminGetMemoryEvalOptions.core";
import { buildAdminGetMemoryEvalRunQuery } from "@gram/admin-client/react-query/adminGetMemoryEvalRun.core";
import {
  buildAdminListMemoryEvalRunsInfiniteQuery,
  type AdminListMemoryEvalRunsPageParams,
} from "@gram/admin-client/react-query/adminListMemoryEvalRuns.core";
import { buildAdminStartMemoryEvalRunMutation } from "@gram/admin-client/react-query/adminStartMemoryEvalRun";

import { redirecting, redirectingClient } from "@/lib/adminSdkClient";

const runsPageSize = 50;

function createOptionsQuery() {
  const generated = buildAdminGetMemoryEvalOptionsQuery(redirectingClient());
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
  });
}

/** memoryEvalOptionsQuery reads the defaults, allowed models and caps. */
export function memoryEvalOptionsQuery(): ReturnType<
  typeof createOptionsQuery
> {
  return createOptionsQuery();
}

function createRunsQuery() {
  const generated = buildAdminListMemoryEvalRunsInfiniteQuery(
    redirectingClient(),
    { limit: runsPageSize },
  );
  return infiniteQueryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    initialPageParam: undefined as AdminListMemoryEvalRunsPageParams,
    getNextPageParam: (lastPage) => lastPage["~next"],
  });
}

/** memoryEvalRunsQuery pages through runs, newest first, without reports. */
export function memoryEvalRunsQuery(): ReturnType<typeof createRunsQuery> {
  return createRunsQuery();
}

function createRunQuery(id: string) {
  const generated = buildAdminGetMemoryEvalRunQuery(redirectingClient(), {
    id,
  });
  return queryOptions({
    ...generated,
    queryFn: (context) => redirecting(generated.queryFn(context)),
    // A finished run's report never changes.
    staleTime: Infinity,
  });
}

/** memoryEvalRunQuery reads one run with its report. */
export function memoryEvalRunQuery(
  id: string,
): ReturnType<typeof createRunQuery> {
  return createRunQuery(id);
}

/**
 * startMemoryEvalRun queues a run. It spends model credit, so it is never
 * retried: a retry after a timeout could start a second run.
 */
export function startMemoryEvalRun(
  config: AdminMemoryEvalRunConfig,
): Promise<AdminMemoryEvalRun> {
  return redirecting(
    buildAdminStartMemoryEvalRunMutation(redirectingClient(), {
      retries: { strategy: "none" },
    }).mutationFn({ request: { config } }),
  );
}

/**
 * invalidateMemoryEvals re-reads runs and options after a start attempt: a
 * refused start means another run appeared, and a failed one may still have
 * used a slot of the daily limit.
 */
export async function invalidateMemoryEvals(
  client: QueryClient,
): Promise<void> {
  await Promise.all([
    client.invalidateQueries({ queryKey: memoryEvalRunsQuery().queryKey }),
    client.invalidateQueries({
      queryKey: queryKeyAdminGetMemoryEvalOptions(),
    }),
  ]);
}
