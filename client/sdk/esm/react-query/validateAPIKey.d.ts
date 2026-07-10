import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ValidateAPIKeyRequest, ValidateAPIKeySecurity } from "../models/operations/validateapikey.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildValidateAPIKeyQuery, prefetchValidateAPIKey, queryKeyValidateAPIKey, ValidateAPIKeyQueryData } from "./validateAPIKey.core.js";
export { buildValidateAPIKeyQuery, prefetchValidateAPIKey, queryKeyValidateAPIKey, type ValidateAPIKeyQueryData, };
export type ValidateAPIKeyQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * verifyKey keys
 *
 * @remarks
 * Verify an api key
 */
export declare function useValidateAPIKey(request?: ValidateAPIKeyRequest | undefined, security?: ValidateAPIKeySecurity | undefined, options?: QueryHookOptions<ValidateAPIKeyQueryData, ValidateAPIKeyQueryError>): UseQueryResult<ValidateAPIKeyQueryData, ValidateAPIKeyQueryError>;
/**
 * verifyKey keys
 *
 * @remarks
 * Verify an api key
 */
export declare function useValidateAPIKeySuspense(request?: ValidateAPIKeyRequest | undefined, security?: ValidateAPIKeySecurity | undefined, options?: SuspenseQueryHookOptions<ValidateAPIKeyQueryData, ValidateAPIKeyQueryError>): UseSuspenseQueryResult<ValidateAPIKeyQueryData, ValidateAPIKeyQueryError>;
export declare function setValidateAPIKeyData(client: QueryClient, queryKeyBase: [parameters: {
    gramKey?: string | undefined;
}], data: ValidateAPIKeyQueryData): ValidateAPIKeyQueryData | undefined;
export declare function invalidateValidateAPIKey(client: QueryClient, queryKeyBase: TupleToPrefixes<[parameters: {
    gramKey?: string | undefined;
}]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllValidateAPIKey(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=validateAPIKey.d.ts.map