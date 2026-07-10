import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RegisterRequest, RegisterSecurity } from "../models/operations/register.js";
import { MutationHookOptions } from "./_types.js";
export type RegisterMutationVariables = {
    request: RegisterRequest;
    security?: RegisterSecurity | undefined;
    options?: RequestOptions;
};
export type RegisterMutationData = void;
export type RegisterMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * register auth
 *
 * @remarks
 * Register a new org for a user with their session information.
 */
export declare function useRegisterMutation(options?: MutationHookOptions<RegisterMutationData, RegisterMutationError, RegisterMutationVariables>): UseMutationResult<RegisterMutationData, RegisterMutationError, RegisterMutationVariables>;
export declare function mutationKeyRegister(): MutationKey;
export declare function buildRegisterMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RegisterMutationVariables) => Promise<RegisterMutationData>;
};
//# sourceMappingURL=register.d.ts.map