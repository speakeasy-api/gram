import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListEnvironmentsRequest, ListEnvironmentsSecurity } from "../models/operations/listenvironments.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListEnvironmentsQuery, ListEnvironmentsQueryData, prefetchListEnvironments, queryKeyListEnvironments } from "./listEnvironments.core.js";
export { buildListEnvironmentsQuery, type ListEnvironmentsQueryData, prefetchListEnvironments, queryKeyListEnvironments, };
export type ListEnvironmentsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listEnvironments environments
 *
 * @remarks
 * List all environments for an organization
 */
export declare function useListEnvironments(request?: ListEnvironmentsRequest | undefined, security?: ListEnvironmentsSecurity | undefined, options?: QueryHookOptions<ListEnvironmentsQueryData, ListEnvironmentsQueryError>): UseQueryResult<ListEnvironmentsQueryData, ListEnvironmentsQueryError>;
/**
 * listEnvironments environments
 *
 * @remarks
 * List all environments for an organization
 */
export declare function useListEnvironmentsSuspense(request?: ListEnvironmentsRequest | undefined, security?: ListEnvironmentsSecurity | undefined, options?: SuspenseQueryHookOptions<ListEnvironmentsQueryData, ListEnvironmentsQueryError>): UseSuspenseQueryResult<ListEnvironmentsQueryData, ListEnvironmentsQueryError>;
export declare function setListEnvironmentsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListEnvironmentsQueryData): ListEnvironmentsQueryData | undefined;
export declare function invalidateListEnvironments(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListEnvironments(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listEnvironments.d.ts.map