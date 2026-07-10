import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListDeploymentsRequest, ListDeploymentsSecurity } from "../models/operations/listdeployments.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListDeploymentsQuery, ListDeploymentsQueryData, prefetchListDeployments, queryKeyListDeployments } from "./listDeployments.core.js";
export { buildListDeploymentsQuery, type ListDeploymentsQueryData, prefetchListDeployments, queryKeyListDeployments, };
export type ListDeploymentsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listDeployments deployments
 *
 * @remarks
 * List all deployments in descending order of creation.
 */
export declare function useListDeployments(request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: QueryHookOptions<ListDeploymentsQueryData, ListDeploymentsQueryError>): UseQueryResult<ListDeploymentsQueryData, ListDeploymentsQueryError>;
/**
 * listDeployments deployments
 *
 * @remarks
 * List all deployments in descending order of creation.
 */
export declare function useListDeploymentsSuspense(request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: SuspenseQueryHookOptions<ListDeploymentsQueryData, ListDeploymentsQueryError>): UseSuspenseQueryResult<ListDeploymentsQueryData, ListDeploymentsQueryError>;
export declare function setListDeploymentsData(client: QueryClient, queryKeyBase: [
    parameters: {
        cursor?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListDeploymentsQueryData): ListDeploymentsQueryData | undefined;
export declare function invalidateListDeployments(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        cursor?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListDeployments(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listDeployments.d.ts.map