import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListUserSessionFacetsResult } from "../models/components/listusersessionfacetsresult.js";
import {
  ListUserSessionFacetsRequest,
  ListUserSessionFacetsSecurity,
} from "../models/operations/listusersessionfacets.js";
export type UserSessionFacetsQueryData = ListUserSessionFacetsResult;
export declare function prefetchUserSessionFacets(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListUserSessionFacetsRequest | undefined,
  security?: ListUserSessionFacetsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildUserSessionFacetsQuery(
  client$: GramCore,
  request?: ListUserSessionFacetsRequest | undefined,
  security?: ListUserSessionFacetsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<UserSessionFacetsQueryData>;
};
export declare function queryKeyUserSessionFacets(parameters: {
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionFacets.core.d.ts.map
