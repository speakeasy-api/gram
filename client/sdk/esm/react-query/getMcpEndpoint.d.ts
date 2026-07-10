import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetMcpEndpointRequest, GetMcpEndpointSecurity } from "../models/operations/getmcpendpoint.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetMcpEndpointQuery, GetMcpEndpointQueryData, prefetchGetMcpEndpoint, queryKeyGetMcpEndpoint } from "./getMcpEndpoint.core.js";
export { buildGetMcpEndpointQuery, type GetMcpEndpointQueryData, prefetchGetMcpEndpoint, queryKeyGetMcpEndpoint, };
export type GetMcpEndpointQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.
 */
export declare function useGetMcpEndpoint(request?: GetMcpEndpointRequest | undefined, security?: GetMcpEndpointSecurity | undefined, options?: QueryHookOptions<GetMcpEndpointQueryData, GetMcpEndpointQueryError>): UseQueryResult<GetMcpEndpointQueryData, GetMcpEndpointQueryError>;
/**
 * getMcpEndpoint mcpEndpoints
 *
 * @remarks
 * Get an MCP endpoint by id or by (custom_domain_id, slug). Provide either id, or slug with an optional custom_domain_id — not both.
 */
export declare function useGetMcpEndpointSuspense(request?: GetMcpEndpointRequest | undefined, security?: GetMcpEndpointSecurity | undefined, options?: SuspenseQueryHookOptions<GetMcpEndpointQueryData, GetMcpEndpointQueryError>): UseSuspenseQueryResult<GetMcpEndpointQueryData, GetMcpEndpointQueryError>;
export declare function setGetMcpEndpointData(client: QueryClient, queryKeyBase: [
    parameters: {
        id?: string | undefined;
        customDomainId?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: GetMcpEndpointQueryData): GetMcpEndpointQueryData | undefined;
export declare function invalidateGetMcpEndpoint(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id?: string | undefined;
        customDomainId?: string | undefined;
        slug?: string | undefined;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetMcpEndpoint(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getMcpEndpoint.d.ts.map