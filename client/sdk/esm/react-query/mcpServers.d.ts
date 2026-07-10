import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListMcpServersRequest, ListMcpServersSecurity } from "../models/operations/listmcpservers.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildMcpServersQuery, McpServersQueryData, prefetchMcpServers, queryKeyMcpServers } from "./mcpServers.core.js";
export { buildMcpServersQuery, type McpServersQueryData, prefetchMcpServers, queryKeyMcpServers, };
export type McpServersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listMcpServers mcpServers
 *
 * @remarks
 * List MCP servers for a project. Accepts optional remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id filters to scope the result to a single backend; at most one filter may be supplied since the backends are mutually exclusive.
 */
export declare function useMcpServers(request?: ListMcpServersRequest | undefined, security?: ListMcpServersSecurity | undefined, options?: QueryHookOptions<McpServersQueryData, McpServersQueryError>): UseQueryResult<McpServersQueryData, McpServersQueryError>;
/**
 * listMcpServers mcpServers
 *
 * @remarks
 * List MCP servers for a project. Accepts optional remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id filters to scope the result to a single backend; at most one filter may be supplied since the backends are mutually exclusive.
 */
export declare function useMcpServersSuspense(request?: ListMcpServersRequest | undefined, security?: ListMcpServersSecurity | undefined, options?: SuspenseQueryHookOptions<McpServersQueryData, McpServersQueryError>): UseSuspenseQueryResult<McpServersQueryData, McpServersQueryError>;
export declare function setMcpServersData(client: QueryClient, queryKeyBase: [
    parameters: {
        remoteMcpServerId?: string | undefined;
        tunneledMcpServerId?: string | undefined;
        toolsetId?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: McpServersQueryData): McpServersQueryData | undefined;
export declare function invalidateMcpServers(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        remoteMcpServerId?: string | undefined;
        tunneledMcpServerId?: string | undefined;
        toolsetId?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllMcpServers(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=mcpServers.d.ts.map