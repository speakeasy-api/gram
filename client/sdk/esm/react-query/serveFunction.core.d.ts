import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ServeFunctionRequest,
  ServeFunctionResponse,
  ServeFunctionSecurity,
} from "../models/operations/servefunction.js";
export type ServeFunctionQueryData = ServeFunctionResponse;
export declare function prefetchServeFunction(
  queryClient: QueryClient,
  client$: GramCore,
  request: ServeFunctionRequest,
  security?: ServeFunctionSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildServeFunctionQuery(
  client$: GramCore,
  request: ServeFunctionRequest,
  security?: ServeFunctionSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ServeFunctionQueryData>;
};
export declare function queryKeyServeFunction(parameters: {
  id: string;
  projectId: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=serveFunction.core.d.ts.map
