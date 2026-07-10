import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMcpServerToolFiltersRequest, ListMcpServerToolFiltersSecurity } from "../models/operations/listmcpservertoolfilters.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListMcpServerToolFiltersQuery, ListMcpServerToolFiltersQueryData, prefetchListMcpServerToolFilters, queryKeyListMcpServerToolFilters } from "./listMcpServerToolFilters.core.js";
export { buildListMcpServerToolFiltersQuery, type ListMcpServerToolFiltersQueryData, prefetchListMcpServerToolFilters, queryKeyListMcpServerToolFilters, };
export type ListMcpServerToolFiltersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listToolFilters mcpServers
 *
 * @remarks
 * List the tool filter scopes (tags) available on an MCP server and the tools under each, including tools excluded from all filters. Exactly one of id or slug must be provided. Read-only; reflects the explicit tool variations group resolved from the chain (mcp_servers then toolsets), deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function useListMcpServerToolFilters(request?: ListMcpServerToolFiltersRequest | undefined, security?: ListMcpServerToolFiltersSecurity | undefined, options?: QueryHookOptions<ListMcpServerToolFiltersQueryData, ListMcpServerToolFiltersQueryError>): UseQueryResult<ListMcpServerToolFiltersQueryData, ListMcpServerToolFiltersQueryError>;
/**
 * listToolFilters mcpServers
 *
 * @remarks
 * List the tool filter scopes (tags) available on an MCP server and the tools under each, including tools excluded from all filters. Exactly one of id or slug must be provided. Read-only; reflects the explicit tool variations group resolved from the chain (mcp_servers then toolsets), deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
 */
export declare function useListMcpServerToolFiltersSuspense(request?: ListMcpServerToolFiltersRequest | undefined, security?: ListMcpServerToolFiltersSecurity | undefined, options?: SuspenseQueryHookOptions<ListMcpServerToolFiltersQueryData, ListMcpServerToolFiltersQueryError>): UseSuspenseQueryResult<ListMcpServerToolFiltersQueryData, ListMcpServerToolFiltersQueryError>;
export declare function setListMcpServerToolFiltersData(client: QueryClient, queryKeyBase: [
    parameters: {
        id?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListMcpServerToolFiltersQueryData): ListMcpServerToolFiltersQueryData | undefined;
export declare function invalidateListMcpServerToolFilters(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListMcpServerToolFilters(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listMcpServerToolFilters.d.ts.map