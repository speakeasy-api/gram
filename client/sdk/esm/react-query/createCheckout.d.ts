import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCheckoutRequest, CreateCheckoutSecurity } from "../models/operations/createcheckout.js";
import { MutationHookOptions } from "./_types.js";
export type CreateCheckoutMutationVariables = {
    request?: CreateCheckoutRequest | undefined;
    security?: CreateCheckoutSecurity | undefined;
    options?: RequestOptions;
};
export type CreateCheckoutMutationData = string;
export type CreateCheckoutMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createCheckout usage
 *
 * @remarks
 * Create a checkout link for upgrading to the business plan
 */
export declare function useCreateCheckoutMutation(options?: MutationHookOptions<CreateCheckoutMutationData, CreateCheckoutMutationError, CreateCheckoutMutationVariables>): UseMutationResult<CreateCheckoutMutationData, CreateCheckoutMutationError, CreateCheckoutMutationVariables>;
export declare function mutationKeyCreateCheckout(): MutationKey;
export declare function buildCreateCheckoutMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateCheckoutMutationVariables) => Promise<CreateCheckoutMutationData>;
};
//# sourceMappingURL=createCheckout.d.ts.map