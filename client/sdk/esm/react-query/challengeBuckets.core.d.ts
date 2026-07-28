import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChallengeBucketsResult } from "../models/components/listchallengebucketsresult.js";
import {
  ListChallengeBucketsRequest,
  ListChallengeBucketsSecurity,
  Outcome,
} from "../models/operations/listchallengebuckets.js";
export type ChallengeBucketsQueryData = ListChallengeBucketsResult;
export declare function prefetchChallengeBuckets(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListChallengeBucketsRequest | undefined,
  security?: ListChallengeBucketsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildChallengeBucketsQuery(
  client$: GramCore,
  request?: ListChallengeBucketsRequest | undefined,
  security?: ListChallengeBucketsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ChallengeBucketsQueryData>;
};
export declare function queryKeyChallengeBuckets(parameters: {
  outcome?: Outcome | undefined;
  principalUrn?: string | undefined;
  scope?: string | undefined;
  projectId?: string | undefined;
  resolved?: boolean | undefined;
  limit?: number | undefined;
  offset?: number | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=challengeBuckets.core.d.ts.map
