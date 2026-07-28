import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type GetSlackAppQueryData = components.SlackAppResult;
export declare function prefetchGetSlackApp(
  queryClient: QueryClient,
  client$: GramCore,
  request: operations.GetSlackAppRequest,
  security?: operations.GetSlackAppSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetSlackAppQuery(
  client$: GramCore,
  request: operations.GetSlackAppRequest,
  security?: operations.GetSlackAppSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetSlackAppQueryData>;
};
export declare function queryKeyGetSlackApp(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getSlackApp.core.d.ts.map
