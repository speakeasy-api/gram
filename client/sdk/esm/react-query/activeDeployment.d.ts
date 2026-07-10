import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetActiveDeploymentRequest, GetActiveDeploymentSecurity } from "../models/operations/getactivedeployment.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { ActiveDeploymentQueryData, buildActiveDeploymentQuery, prefetchActiveDeployment, queryKeyActiveDeployment } from "./activeDeployment.core.js";
export { type ActiveDeploymentQueryData, buildActiveDeploymentQuery, prefetchActiveDeployment, queryKeyActiveDeployment, };
export type ActiveDeploymentQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getActiveDeployment deployments
 *
 * @remarks
 * Get the active deployment for a project.
 */
export declare function useActiveDeployment(request?: GetActiveDeploymentRequest | undefined, security?: GetActiveDeploymentSecurity | undefined, options?: QueryHookOptions<ActiveDeploymentQueryData, ActiveDeploymentQueryError>): UseQueryResult<ActiveDeploymentQueryData, ActiveDeploymentQueryError>;
/**
 * getActiveDeployment deployments
 *
 * @remarks
 * Get the active deployment for a project.
 */
export declare function useActiveDeploymentSuspense(request?: GetActiveDeploymentRequest | undefined, security?: GetActiveDeploymentSecurity | undefined, options?: SuspenseQueryHookOptions<ActiveDeploymentQueryData, ActiveDeploymentQueryError>): UseSuspenseQueryResult<ActiveDeploymentQueryData, ActiveDeploymentQueryError>;
export declare function setActiveDeploymentData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ActiveDeploymentQueryData): ActiveDeploymentQueryData | undefined;
export declare function invalidateActiveDeployment(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllActiveDeployment(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=activeDeployment.d.ts.map