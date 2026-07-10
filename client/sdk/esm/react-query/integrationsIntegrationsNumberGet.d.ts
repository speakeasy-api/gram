import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { IntegrationsNumberGetRequest, IntegrationsNumberGetSecurity } from "../models/operations/integrationsnumberget.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildIntegrationsIntegrationsNumberGetQuery, IntegrationsIntegrationsNumberGetQueryData, prefetchIntegrationsIntegrationsNumberGet, queryKeyIntegrationsIntegrationsNumberGet } from "./integrationsIntegrationsNumberGet.core.js";
export { buildIntegrationsIntegrationsNumberGetQuery, type IntegrationsIntegrationsNumberGetQueryData, prefetchIntegrationsIntegrationsNumberGet, queryKeyIntegrationsIntegrationsNumberGet, };
export type IntegrationsIntegrationsNumberGetQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * get integrations
 *
 * @remarks
 * Get a third-party integration by ID or name.
 */
export declare function useIntegrationsIntegrationsNumberGet(request?: IntegrationsNumberGetRequest | undefined, security?: IntegrationsNumberGetSecurity | undefined, options?: QueryHookOptions<IntegrationsIntegrationsNumberGetQueryData, IntegrationsIntegrationsNumberGetQueryError>): UseQueryResult<IntegrationsIntegrationsNumberGetQueryData, IntegrationsIntegrationsNumberGetQueryError>;
/**
 * get integrations
 *
 * @remarks
 * Get a third-party integration by ID or name.
 */
export declare function useIntegrationsIntegrationsNumberGetSuspense(request?: IntegrationsNumberGetRequest | undefined, security?: IntegrationsNumberGetSecurity | undefined, options?: SuspenseQueryHookOptions<IntegrationsIntegrationsNumberGetQueryData, IntegrationsIntegrationsNumberGetQueryError>): UseSuspenseQueryResult<IntegrationsIntegrationsNumberGetQueryData, IntegrationsIntegrationsNumberGetQueryError>;
export declare function setIntegrationsIntegrationsNumberGetData(client: QueryClient, queryKeyBase: [
    parameters: {
        id?: string | undefined;
        name?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: IntegrationsIntegrationsNumberGetQueryData): IntegrationsIntegrationsNumberGetQueryData | undefined;
export declare function invalidateIntegrationsIntegrationsNumberGet(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id?: string | undefined;
        name?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllIntegrationsIntegrationsNumberGet(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=integrationsIntegrationsNumberGet.d.ts.map