import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateTopUpCheckoutRequest, CreateTopUpCheckoutSecurity } from "../models/operations/createtopupcheckout.js";
import { MutationHookOptions } from "./_types.js";
export type CreateTopUpCheckoutMutationVariables = {
    request?: CreateTopUpCheckoutRequest | undefined;
    security?: CreateTopUpCheckoutSecurity | undefined;
    options?: RequestOptions;
};
export type CreateTopUpCheckoutMutationData = string;
export type CreateTopUpCheckoutMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createTopUpCheckout usage
 *
 * @remarks
 * Create a checkout link for a one-time credit top-up purchase
 */
export declare function useCreateTopUpCheckoutMutation(options?: MutationHookOptions<CreateTopUpCheckoutMutationData, CreateTopUpCheckoutMutationError, CreateTopUpCheckoutMutationVariables>): UseMutationResult<CreateTopUpCheckoutMutationData, CreateTopUpCheckoutMutationError, CreateTopUpCheckoutMutationVariables>;
export declare function mutationKeyCreateTopUpCheckout(): MutationKey;
export declare function buildCreateTopUpCheckoutMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateTopUpCheckoutMutationVariables) => Promise<CreateTopUpCheckoutMutationData>;
};
//# sourceMappingURL=createTopUpCheckout.d.ts.map