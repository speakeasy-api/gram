import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListToolsetToolFiltersRequest, ListToolsetToolFiltersSecurity } from "../models/operations/listtoolsettoolfilters.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListToolsetToolFiltersQuery, ListToolsetToolFiltersQueryData, prefetchListToolsetToolFilters, queryKeyListToolsetToolFilters } from "./listToolsetToolFilters.core.js";
export { buildListToolsetToolFiltersQuery, type ListToolsetToolFiltersQueryData, prefetchListToolsetToolFilters, queryKeyListToolsetToolFilters, };
export type ListToolsetToolFiltersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listToolFilters toolsets
 *
 * @remarks
 * List the tool filter scopes (tags) available on a toolset-backed MCP server and the tools under each, including tools excluded from all filters. Read-only; reflects the explicit tool variations group configured on the toolset, deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function useListToolsetToolFilters(request: ListToolsetToolFiltersRequest, security?: ListToolsetToolFiltersSecurity | undefined, options?: QueryHookOptions<ListToolsetToolFiltersQueryData, ListToolsetToolFiltersQueryError>): UseQueryResult<ListToolsetToolFiltersQueryData, ListToolsetToolFiltersQueryError>;
/**
 * listToolFilters toolsets
 *
 * @remarks
 * List the tool filter scopes (tags) available on a toolset-backed MCP server and the tools under each, including tools excluded from all filters. Read-only; reflects the explicit tool variations group configured on the toolset, deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function useListToolsetToolFiltersSuspense(request: ListToolsetToolFiltersRequest, security?: ListToolsetToolFiltersSecurity | undefined, options?: SuspenseQueryHookOptions<ListToolsetToolFiltersQueryData, ListToolsetToolFiltersQueryError>): UseSuspenseQueryResult<ListToolsetToolFiltersQueryData, ListToolsetToolFiltersQueryError>;
export declare function setListToolsetToolFiltersData(client: QueryClient, queryKeyBase: [
    parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListToolsetToolFiltersQueryData): ListToolsetToolFiltersQueryData | undefined;
export declare function invalidateListToolsetToolFilters(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        slug: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListToolsetToolFilters(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listToolsetToolFilters.d.ts.map