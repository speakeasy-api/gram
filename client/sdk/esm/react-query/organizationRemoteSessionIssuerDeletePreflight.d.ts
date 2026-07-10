import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetOrganizationRemoteSessionIssuerDeletePreflightRequest, GetOrganizationRemoteSessionIssuerDeletePreflightSecurity } from "../models/operations/getorganizationremotesessionissuerdeletepreflight.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildOrganizationRemoteSessionIssuerDeletePreflightQuery, OrganizationRemoteSessionIssuerDeletePreflightQueryData, prefetchOrganizationRemoteSessionIssuerDeletePreflight, queryKeyOrganizationRemoteSessionIssuerDeletePreflight } from "./organizationRemoteSessionIssuerDeletePreflight.core.js";
export { buildOrganizationRemoteSessionIssuerDeletePreflightQuery, type OrganizationRemoteSessionIssuerDeletePreflightQueryData, prefetchOrganizationRemoteSessionIssuerDeletePreflight, queryKeyOrganizationRemoteSessionIssuerDeletePreflight, };
export type OrganizationRemoteSessionIssuerDeletePreflightQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getIssuerDeletePreflight organizationRemoteSessionIssuers
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuerDeletePreflight(request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest, security?: GetOrganizationRemoteSessionIssuerDeletePreflightSecurity | undefined, options?: QueryHookOptions<OrganizationRemoteSessionIssuerDeletePreflightQueryData, OrganizationRemoteSessionIssuerDeletePreflightQueryError>): UseQueryResult<OrganizationRemoteSessionIssuerDeletePreflightQueryData, OrganizationRemoteSessionIssuerDeletePreflightQueryError>;
/**
 * getIssuerDeletePreflight organizationRemoteSessionIssuers
 *
 * @remarks
 * Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.
 */
export declare function useOrganizationRemoteSessionIssuerDeletePreflightSuspense(request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest, security?: GetOrganizationRemoteSessionIssuerDeletePreflightSecurity | undefined, options?: SuspenseQueryHookOptions<OrganizationRemoteSessionIssuerDeletePreflightQueryData, OrganizationRemoteSessionIssuerDeletePreflightQueryError>): UseSuspenseQueryResult<OrganizationRemoteSessionIssuerDeletePreflightQueryData, OrganizationRemoteSessionIssuerDeletePreflightQueryError>;
export declare function setOrganizationRemoteSessionIssuerDeletePreflightData(client: QueryClient, queryKeyBase: [
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
], data: OrganizationRemoteSessionIssuerDeletePreflightQueryData): OrganizationRemoteSessionIssuerDeletePreflightQueryData | undefined;
export declare function invalidateOrganizationRemoteSessionIssuerDeletePreflight(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllOrganizationRemoteSessionIssuerDeletePreflight(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=organizationRemoteSessionIssuerDeletePreflight.d.ts.map