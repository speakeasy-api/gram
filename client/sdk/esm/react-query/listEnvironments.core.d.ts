import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListEnvironmentsResult } from "../models/components/listenvironmentsresult.js";
import {
  ListEnvironmentsRequest,
  ListEnvironmentsSecurity,
} from "../models/operations/listenvironments.js";
export type ListEnvironmentsQueryData = ListEnvironmentsResult;
export declare function prefetchListEnvironments(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListEnvironmentsRequest | undefined,
  security?: ListEnvironmentsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListEnvironmentsQuery(
  client$: GramCore,
  request?: ListEnvironmentsRequest | undefined,
  security?: ListEnvironmentsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ListEnvironmentsQueryData>;
};
export declare function queryKeyListEnvironments(parameters: {
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listEnvironments.core.d.ts.map
