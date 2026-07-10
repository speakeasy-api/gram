import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetGcpIamCredentialRequest, GetGcpIamCredentialSecurity } from "../models/operations/getgcpiamcredential.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildGetGcpIamCredentialQuery, GetGcpIamCredentialQueryData, prefetchGetGcpIamCredential, queryKeyGetGcpIamCredential } from "./getGcpIamCredential.core.js";
export { buildGetGcpIamCredentialQuery, type GetGcpIamCredentialQueryData, prefetchGetGcpIamCredential, queryKeyGetGcpIamCredential, };
export type GetGcpIamCredentialQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getGcpIamCredential externalCredentials
 *
 * @remarks
 * Get a GCP IAM external credential by ID. Requires org:read.
 */
export declare function useGetGcpIamCredential(request: GetGcpIamCredentialRequest, security?: GetGcpIamCredentialSecurity | undefined, options?: QueryHookOptions<GetGcpIamCredentialQueryData, GetGcpIamCredentialQueryError>): UseQueryResult<GetGcpIamCredentialQueryData, GetGcpIamCredentialQueryError>;
/**
 * getGcpIamCredential externalCredentials
 *
 * @remarks
 * Get a GCP IAM external credential by ID. Requires org:read.
 */
export declare function useGetGcpIamCredentialSuspense(request: GetGcpIamCredentialRequest, security?: GetGcpIamCredentialSecurity | undefined, options?: SuspenseQueryHookOptions<GetGcpIamCredentialQueryData, GetGcpIamCredentialQueryError>): UseSuspenseQueryResult<GetGcpIamCredentialQueryData, GetGcpIamCredentialQueryError>;
export declare function setGetGcpIamCredentialData(client: QueryClient, queryKeyBase: [parameters: {
    id: string;
    gramSession?: string | undefined;
}], data: GetGcpIamCredentialQueryData): GetGcpIamCredentialQueryData | undefined;
export declare function invalidateGetGcpIamCredential(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        id: string;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllGetGcpIamCredential(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=getGcpIamCredential.d.ts.map