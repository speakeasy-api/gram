import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUserGrantsResult } from "../models/components/listusergrantsresult.js";
import {
  ListGrantsRequest,
  ListGrantsSecurity,
} from "../models/operations/listgrants.js";
export type GrantsQueryData = ListUserGrantsResult;
export declare function prefetchGrants(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListGrantsRequest | undefined,
  security?: ListGrantsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGrantsQuery(
  client$: GramCore,
  request?: ListGrantsRequest | undefined,
  security?: ListGrantsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GrantsQueryData>;
};
export declare function queryKeyGrants(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=grants.core.d.ts.map
