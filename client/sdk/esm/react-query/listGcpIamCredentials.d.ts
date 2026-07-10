import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListGcpIamCredentialsRequest, ListGcpIamCredentialsSecurity } from "../models/operations/listgcpiamcredentials.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListGcpIamCredentialsQuery, ListGcpIamCredentialsQueryData, prefetchListGcpIamCredentials, queryKeyListGcpIamCredentials } from "./listGcpIamCredentials.core.js";
export { buildListGcpIamCredentialsQuery, type ListGcpIamCredentialsQueryData, prefetchListGcpIamCredentials, queryKeyListGcpIamCredentials, };
export type ListGcpIamCredentialsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listGcpIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's GCP IAM external credentials. Requires org:read.
 */
export declare function useListGcpIamCredentials(request?: ListGcpIamCredentialsRequest | undefined, security?: ListGcpIamCredentialsSecurity | undefined, options?: QueryHookOptions<ListGcpIamCredentialsQueryData, ListGcpIamCredentialsQueryError>): UseQueryResult<ListGcpIamCredentialsQueryData, ListGcpIamCredentialsQueryError>;
/**
 * listGcpIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's GCP IAM external credentials. Requires org:read.
 */
export declare function useListGcpIamCredentialsSuspense(request?: ListGcpIamCredentialsRequest | undefined, security?: ListGcpIamCredentialsSecurity | undefined, options?: SuspenseQueryHookOptions<ListGcpIamCredentialsQueryData, ListGcpIamCredentialsQueryError>): UseSuspenseQueryResult<ListGcpIamCredentialsQueryData, ListGcpIamCredentialsQueryError>;
export declare function setListGcpIamCredentialsData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: ListGcpIamCredentialsQueryData): ListGcpIamCredentialsQueryData | undefined;
export declare function invalidateListGcpIamCredentials(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListGcpIamCredentials(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listGcpIamCredentials.d.ts.map