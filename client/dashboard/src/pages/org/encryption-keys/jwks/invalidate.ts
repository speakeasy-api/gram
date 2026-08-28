import { invalidateAllAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { invalidateAllGetJsonWebKeySet } from "@gram/client/react-query/getJsonWebKeySet";
import { invalidateAllListJsonWebKeys } from "@gram/client/react-query/listJsonWebKeys";
import { invalidateAllListJsonWebKeySets } from "@gram/client/react-query/listJsonWebKeySets";
import type { QueryClient } from "@tanstack/react-query";

// Every write to a set or one of its keys changes what the set list, the set's
// detail, its Keys tab and its audit history show, so all of them refresh
// together.
export async function invalidateSet(queryClient: QueryClient): Promise<void> {
  await Promise.all([
    invalidateAllListJsonWebKeys(queryClient),
    invalidateAllGetJsonWebKeySet(queryClient),
    invalidateAllListJsonWebKeySets(queryClient),
    invalidateAllAuditLogs(queryClient),
  ]);
}
