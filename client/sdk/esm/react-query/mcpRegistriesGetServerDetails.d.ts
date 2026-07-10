import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetMCPServerDetailsRequest, GetMCPServerDetailsSecurity } from "../models/operations/getmcpserverdetails.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildMcpRegistriesGetServerDetailsQuery, McpRegistriesGetServerDetailsQueryData, prefetchMcpRegistriesGetServerDetails, queryKeyMcpRegistriesGetServerDetails } from "./mcpRegistriesGetServerDetails.core.js";
export { buildMcpRegistriesGetServerDetailsQuery, type McpRegistriesGetServerDetailsQueryData, prefetchMcpRegistriesGetServerDetails, queryKeyMcpRegistriesGetServerDetails, };
export type McpRegistriesGetServerDetailsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getServerDetails mcpRegistries
 *
 * @remarks
 * Get detailed information about an MCP server including remotes
 */
export declare function useMcpRegistriesGetServerDetails(request: GetMCPServerDetailsRequest, security?: GetMCPServerDetailsSecurity | undefined, options?: QueryHookOptions<McpRegistriesGetServerDetailsQueryData, McpRegistriesGetServerDetailsQueryError>): UseQueryResult<McpRegistriesGetServerDetailsQueryData, McpRegistriesGetServerDetailsQueryError>;
/**
 * getServerDetails mcpRegistries
 *
 * @remarks
 * Get detailed information about an MCP server including remotes
 */
export declare function useMcpRegistriesGetServerDetailsSuspense(request: GetMCPServerDetailsRequest, security?: GetMCPServerDetailsSecurity | undefined, options?: SuspenseQueryHookOptions<McpRegistriesGetServerDetailsQueryData, McpRegistriesGetServerDetailsQueryError>): UseSuspenseQueryResult<McpRegistriesGetServerDetailsQueryData, McpRegistriesGetServerDetailsQueryError>;
export declare function setMcpRegistriesGetServerDetailsData(client: QueryClient, queryKeyBase: [
    parameters: {
        registryId: string;
        serverSpecifier: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: McpRegistriesGetServerDetailsQueryData): McpRegistriesGetServerDetailsQueryData | undefined;
export declare function invalidateMcpRegistriesGetServerDetails(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        registryId: string;
        serverSpecifier: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllMcpRegistriesGetServerDetails(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=mcpRegistriesGetServerDetails.d.ts.map