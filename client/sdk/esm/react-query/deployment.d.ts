import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetDeploymentRequest, GetDeploymentSecurity } from "../models/operations/getdeployment.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildDeploymentQuery, DeploymentQueryData, prefetchDeployment, queryKeyDeployment } from "./deployment.core.js";
export { buildDeploymentQuery, type DeploymentQueryData, prefetchDeployment, queryKeyDeployment, };
export type DeploymentQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getDeployment deployments
 *
 * @remarks
 * Get a deployment by its ID.
 */
export declare function useDeployment(request: GetDeploymentRequest, security?: GetDeploymentSecurity | undefined, options?: QueryHookOptions<DeploymentQueryData, DeploymentQueryError>): UseQueryResult<DeploymentQueryData, DeploymentQueryError>;
/**
 * getDeployment deployments
 *
 * @remarks
 * Get a deployment by its ID.
 */
export declare function useDeploymentSuspense(request: GetDeploymentRequest, security?: GetDeploymentSecurity | undefined, options?: SuspenseQueryHookOptions<DeploymentQueryData, DeploymentQueryError>): UseSuspenseQueryResult<DeploymentQueryData, DeploymentQueryError>;
export declare function setDeploymentData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: DeploymentQueryData): DeploymentQueryData | undefined;
export declare function invalidateDeployment(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllDeployment(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=deployment.d.ts.map