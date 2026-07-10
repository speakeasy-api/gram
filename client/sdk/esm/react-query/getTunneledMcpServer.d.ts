import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetTunneledMcpServerRequest, GetTunneledMcpServerSecurity } from "../models/operations/gettunneledmcpserver.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetTunneledMcpServerQuery, GetTunneledMcpServerQueryData, prefetchGetTunneledMcpServer, queryKeyGetTunneledMcpServer } from "./getTunneledMcpServer.core.js";
export { buildGetTunneledMcpServerQuery, type GetTunneledMcpServerQueryData, prefetchGetTunneledMcpServer, queryKeyGetTunneledMcpServer, };
export type GetTunneledMcpServerQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getServer tunneledMcp
 *
 * @remarks
 * Get a tunneled MCP server by ID
 */
export declare function useGetTunneledMcpServer(request: GetTunneledMcpServerRequest, security?: GetTunneledMcpServerSecurity | undefined, options?: QueryHookOptions<GetTunneledMcpServerQueryData, GetTunneledMcpServerQueryError>): UseQueryResult<GetTunneledMcpServerQueryData, GetTunneledMcpServerQueryError>;
/**
 * getServer tunneledMcp
 *
 * @remarks
 * Get a tunneled MCP server by ID
 */
export declare function useGetTunneledMcpServerSuspense(request: GetTunneledMcpServerRequest, security?: GetTunneledMcpServerSecurity | undefined, options?: SuspenseQueryHookOptions<GetTunneledMcpServerQueryData, GetTunneledMcpServerQueryError>): UseSuspenseQueryResult<GetTunneledMcpServerQueryData, GetTunneledMcpServerQueryError>;
export declare function setGetTunneledMcpServerData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetTunneledMcpServerQueryData): GetTunneledMcpServerQueryData | undefined;
export declare function invalidateGetTunneledMcpServer(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetTunneledMcpServer(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getTunneledMcpServer.d.ts.map