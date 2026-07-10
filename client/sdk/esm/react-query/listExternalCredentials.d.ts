import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListExternalCredentialsRequest, ListExternalCredentialsSecurity, Provider } from "../models/operations/listexternalcredentials.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListExternalCredentialsQuery, ListExternalCredentialsQueryData, prefetchListExternalCredentials, queryKeyListExternalCredentials } from "./listExternalCredentials.core.js";
export { buildListExternalCredentialsQuery, type ListExternalCredentialsQueryData, prefetchListExternalCredentials, queryKeyListExternalCredentials, };
export type ListExternalCredentialsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listExternalCredentials externalCredentials
 *
 * @remarks
 * List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.
 */
export declare function useListExternalCredentials(request?: ListExternalCredentialsRequest | undefined, security?: ListExternalCredentialsSecurity | undefined, options?: QueryHookOptions<ListExternalCredentialsQueryData, ListExternalCredentialsQueryError>): UseQueryResult<ListExternalCredentialsQueryData, ListExternalCredentialsQueryError>;
/**
 * listExternalCredentials externalCredentials
 *
 * @remarks
 * List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.
 */
export declare function useListExternalCredentialsSuspense(request?: ListExternalCredentialsRequest | undefined, security?: ListExternalCredentialsSecurity | undefined, options?: SuspenseQueryHookOptions<ListExternalCredentialsQueryData, ListExternalCredentialsQueryError>): UseSuspenseQueryResult<ListExternalCredentialsQueryData, ListExternalCredentialsQueryError>;
export declare function setListExternalCredentialsData(client: QueryClient, queryKeyBase: [
    parameters: {
        provider?: Provider | undefined;
        gramSession?: string | undefined;
    }
], data: ListExternalCredentialsQueryData): ListExternalCredentialsQueryData | undefined;
export declare function invalidateListExternalCredentials(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        provider?: Provider | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListExternalCredentials(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listExternalCredentials.d.ts.map