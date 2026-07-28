import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
import {
  GetToolsetEnvironmentRequest,
  GetToolsetEnvironmentSecurity,
} from "../models/operations/gettoolsetenvironment.js";
export type GetToolsetEnvironmentQueryData = Environment;
export declare function prefetchGetToolsetEnvironment(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetToolsetEnvironmentRequest,
  security?: GetToolsetEnvironmentSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetToolsetEnvironmentQuery(
  client$: GramCore,
  request: GetToolsetEnvironmentRequest,
  security?: GetToolsetEnvironmentSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetToolsetEnvironmentQueryData>;
};
export declare function queryKeyGetToolsetEnvironment(parameters: {
  toolsetId: string;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getToolsetEnvironment.core.d.ts.map
