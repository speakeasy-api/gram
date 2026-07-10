import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetDeploymentLogsResult } from "../models/components/getdeploymentlogsresult.js";
import {
  GetDeploymentLogsRequest,
  GetDeploymentLogsSecurity,
} from "../models/operations/getdeploymentlogs.js";
export type DeploymentLogsQueryData = GetDeploymentLogsResult;
export declare function prefetchDeploymentLogs(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetDeploymentLogsRequest,
  security?: GetDeploymentLogsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildDeploymentLogsQuery(
  client$: GramCore,
  request: GetDeploymentLogsRequest,
  security?: GetDeploymentLogsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<DeploymentLogsQueryData>;
};
export declare function queryKeyDeploymentLogs(parameters: {
  deploymentId: string;
  cursor?: string | undefined;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=deploymentLogs.core.d.ts.map
