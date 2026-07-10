import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetDeploymentLogsRequest, GetDeploymentLogsSecurity } from "../models/operations/getdeploymentlogs.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildDeploymentLogsQuery, DeploymentLogsQueryData, prefetchDeploymentLogs, queryKeyDeploymentLogs } from "./deploymentLogs.core.js";
export { buildDeploymentLogsQuery, type DeploymentLogsQueryData, prefetchDeploymentLogs, queryKeyDeploymentLogs, };
export type DeploymentLogsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getDeploymentLogs deployments
 *
 * @remarks
 * Get logs for a deployment.
 */
export declare function useDeploymentLogs(request: GetDeploymentLogsRequest, security?: GetDeploymentLogsSecurity | undefined, options?: QueryHookOptions<DeploymentLogsQueryData, DeploymentLogsQueryError>): UseQueryResult<DeploymentLogsQueryData, DeploymentLogsQueryError>;
/**
 * getDeploymentLogs deployments
 *
 * @remarks
 * Get logs for a deployment.
 */
export declare function useDeploymentLogsSuspense(request: GetDeploymentLogsRequest, security?: GetDeploymentLogsSecurity | undefined, options?: SuspenseQueryHookOptions<DeploymentLogsQueryData, DeploymentLogsQueryError>): UseSuspenseQueryResult<DeploymentLogsQueryData, DeploymentLogsQueryError>;
export declare function setDeploymentLogsData(client: QueryClient, queryKeyBase: [
    parameters: {
        deploymentId: string;
        cursor?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: DeploymentLogsQueryData): DeploymentLogsQueryData | undefined;
export declare function invalidateDeploymentLogs(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        deploymentId: string;
        cursor?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllDeploymentLogs(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=deploymentLogs.d.ts.map