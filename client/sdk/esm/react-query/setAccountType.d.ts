import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type SetAccountTypeMutationVariables = {
    request: operations.SetAccountTypeRequest;
    security?: operations.SetAccountTypeSecurity | undefined;
    options?: RequestOptions;
};
export type SetAccountTypeMutationData = void;
export type SetAccountTypeMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * setAccountType organizations
 *
 * @remarks
 * Set a Gram organization's account tier (admin only - requires speakeasy-team API key).
 */
export declare function useSetAccountTypeMutation(options?: MutationHookOptions<SetAccountTypeMutationData, SetAccountTypeMutationError, SetAccountTypeMutationVariables>): UseMutationResult<SetAccountTypeMutationData, SetAccountTypeMutationError, SetAccountTypeMutationVariables>;
export declare function mutationKeySetAccountType(): MutationKey;
export declare function buildSetAccountTypeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: SetAccountTypeMutationVariables) => Promise<SetAccountTypeMutationData>;
};
//# sourceMappingURL=setAccountType.d.ts.map