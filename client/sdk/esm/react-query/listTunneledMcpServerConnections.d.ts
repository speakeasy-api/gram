import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTunneledMcpServerConnectionsRequest, ListTunneledMcpServerConnectionsSecurity } from "../models/operations/listtunneledmcpserverconnections.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListTunneledMcpServerConnectionsQuery, ListTunneledMcpServerConnectionsQueryData, prefetchListTunneledMcpServerConnections, queryKeyListTunneledMcpServerConnections } from "./listTunneledMcpServerConnections.core.js";
export { buildListTunneledMcpServerConnectionsQuery, type ListTunneledMcpServerConnectionsQueryData, prefetchListTunneledMcpServerConnections, queryKeyListTunneledMcpServerConnections, };
export type ListTunneledMcpServerConnectionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listServerConnections tunneledMcp
 *
 * @remarks
 * List live tunnel connections for a tunneled MCP server
 */
export declare function useListTunneledMcpServerConnections(request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: QueryHookOptions<ListTunneledMcpServerConnectionsQueryData, ListTunneledMcpServerConnectionsQueryError>): UseQueryResult<ListTunneledMcpServerConnectionsQueryData, ListTunneledMcpServerConnectionsQueryError>;
/**
 * listServerConnections tunneledMcp
 *
 * @remarks
 * List live tunnel connections for a tunneled MCP server
 */
export declare function useListTunneledMcpServerConnectionsSuspense(request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: SuspenseQueryHookOptions<ListTunneledMcpServerConnectionsQueryData, ListTunneledMcpServerConnectionsQueryError>): UseSuspenseQueryResult<ListTunneledMcpServerConnectionsQueryData, ListTunneledMcpServerConnectionsQueryError>;
export declare function setListTunneledMcpServerConnectionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListTunneledMcpServerConnectionsQueryData): ListTunneledMcpServerConnectionsQueryData | undefined;
export declare function invalidateListTunneledMcpServerConnections(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListTunneledMcpServerConnections(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listTunneledMcpServerConnections.d.ts.map