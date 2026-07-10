import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetDeploymentResult } from "../models/components/getdeploymentresult.js";
import {
  GetDeploymentRequest,
  GetDeploymentSecurity,
} from "../models/operations/getdeployment.js";
export type DeploymentQueryData = GetDeploymentResult;
export declare function prefetchDeployment(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetDeploymentRequest,
  security?: GetDeploymentSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildDeploymentQuery(
  client$: GramCore,
  request: GetDeploymentRequest,
  security?: GetDeploymentSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<DeploymentQueryData>;
};
export declare function queryKeyDeployment(parameters: {
  id: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=deployment.core.d.ts.map
