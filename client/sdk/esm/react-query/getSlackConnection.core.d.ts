import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type GetSlackConnectionQueryData = components.GetSlackConnectionResult;
export declare function prefetchGetSlackConnection(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.GetSlackConnectionRequest | undefined,
  security?: operations.GetSlackConnectionSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetSlackConnectionQuery(
  client$: GramCore,
  request?: operations.GetSlackConnectionRequest | undefined,
  security?: operations.GetSlackConnectionSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetSlackConnectionQueryData>;
};
export declare function queryKeyGetSlackConnection(parameters: {
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getSlackConnection.core.d.ts.map
