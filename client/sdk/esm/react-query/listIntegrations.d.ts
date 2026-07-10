import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListIntegrationsRequest, ListIntegrationsSecurity } from "../models/operations/listintegrations.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListIntegrationsQuery, ListIntegrationsQueryData, prefetchListIntegrations, queryKeyListIntegrations } from "./listIntegrations.core.js";
export { buildListIntegrationsQuery, type ListIntegrationsQueryData, prefetchListIntegrations, queryKeyListIntegrations, };
export type ListIntegrationsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * list integrations
 *
 * @remarks
 * List available third-party integrations.
 */
export declare function useListIntegrations(request?: ListIntegrationsRequest | undefined, security?: ListIntegrationsSecurity | undefined, options?: QueryHookOptions<ListIntegrationsQueryData, ListIntegrationsQueryError>): UseQueryResult<ListIntegrationsQueryData, ListIntegrationsQueryError>;
/**
 * list integrations
 *
 * @remarks
 * List available third-party integrations.
 */
export declare function useListIntegrationsSuspense(request?: ListIntegrationsRequest | undefined, security?: ListIntegrationsSecurity | undefined, options?: SuspenseQueryHookOptions<ListIntegrationsQueryData, ListIntegrationsQueryError>): UseSuspenseQueryResult<ListIntegrationsQueryData, ListIntegrationsQueryError>;
export declare function setListIntegrationsData(client: QueryClient, queryKeyBase: [
    parameters: {
        keywords?: Array<string> | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListIntegrationsQueryData): ListIntegrationsQueryData | undefined;
export declare function invalidateListIntegrations(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        keywords?: Array<string> | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListIntegrations(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listIntegrations.d.ts.map