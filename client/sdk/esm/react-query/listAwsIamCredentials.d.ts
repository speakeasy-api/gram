import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAwsIamCredentialsRequest, ListAwsIamCredentialsSecurity } from "../models/operations/listawsiamcredentials.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAwsIamCredentialsQuery, ListAwsIamCredentialsQueryData, prefetchListAwsIamCredentials, queryKeyListAwsIamCredentials } from "./listAwsIamCredentials.core.js";
export { buildListAwsIamCredentialsQuery, type ListAwsIamCredentialsQueryData, prefetchListAwsIamCredentials, queryKeyListAwsIamCredentials, };
export type ListAwsIamCredentialsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listAwsIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's AWS IAM external credentials. Requires org:read.
 */
export declare function useListAwsIamCredentials(request?: ListAwsIamCredentialsRequest | undefined, security?: ListAwsIamCredentialsSecurity | undefined, options?: QueryHookOptions<ListAwsIamCredentialsQueryData, ListAwsIamCredentialsQueryError>): UseQueryResult<ListAwsIamCredentialsQueryData, ListAwsIamCredentialsQueryError>;
/**
 * listAwsIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's AWS IAM external credentials. Requires org:read.
 */
export declare function useListAwsIamCredentialsSuspense(request?: ListAwsIamCredentialsRequest | undefined, security?: ListAwsIamCredentialsSecurity | undefined, options?: SuspenseQueryHookOptions<ListAwsIamCredentialsQueryData, ListAwsIamCredentialsQueryError>): UseSuspenseQueryResult<ListAwsIamCredentialsQueryData, ListAwsIamCredentialsQueryError>;
export declare function setListAwsIamCredentialsData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: ListAwsIamCredentialsQueryData): ListAwsIamCredentialsQueryData | undefined;
export declare function invalidateListAwsIamCredentials(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAwsIamCredentials(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAwsIamCredentials.d.ts.map