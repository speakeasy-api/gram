import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolsetsRequest, ListToolsetsSecurity } from "../models/operations/listtoolsets.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListToolsetsQuery, ListToolsetsQueryData, prefetchListToolsets, queryKeyListToolsets } from "./listToolsets.core.js";
export { buildListToolsetsQuery, type ListToolsetsQueryData, prefetchListToolsets, queryKeyListToolsets, };
export type ListToolsetsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listToolsets toolsets
 *
 * @remarks
 * List all toolsets for a project
 */
export declare function useListToolsets(request?: ListToolsetsRequest | undefined, security?: ListToolsetsSecurity | undefined, options?: QueryHookOptions<ListToolsetsQueryData, ListToolsetsQueryError>): UseQueryResult<ListToolsetsQueryData, ListToolsetsQueryError>;
/**
 * listToolsets toolsets
 *
 * @remarks
 * List all toolsets for a project
 */
export declare function useListToolsetsSuspense(request?: ListToolsetsRequest | undefined, security?: ListToolsetsSecurity | undefined, options?: SuspenseQueryHookOptions<ListToolsetsQueryData, ListToolsetsQueryError>): UseSuspenseQueryResult<ListToolsetsQueryData, ListToolsetsQueryError>;
export declare function setListToolsetsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListToolsetsQueryData): ListToolsetsQueryData | undefined;
export declare function invalidateListToolsets(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListToolsets(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listToolsets.d.ts.map