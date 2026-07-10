import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCustomerSessionRequest, CreateCustomerSessionSecurity } from "../models/operations/createcustomersession.js";
import { MutationHookOptions } from "./_types.js";
export type CreateCustomerSessionMutationVariables = {
    request?: CreateCustomerSessionRequest | undefined;
    security?: CreateCustomerSessionSecurity | undefined;
    options?: RequestOptions;
};
export type CreateCustomerSessionMutationData = string;
export type CreateCustomerSessionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createCustomerSession usage
 *
 * @remarks
 * Create a customer session for the user
 */
export declare function useCreateCustomerSessionMutation(options?: MutationHookOptions<CreateCustomerSessionMutationData, CreateCustomerSessionMutationError, CreateCustomerSessionMutationVariables>): UseMutationResult<CreateCustomerSessionMutationData, CreateCustomerSessionMutationError, CreateCustomerSessionMutationVariables>;
export declare function mutationKeyCreateCustomerSession(): MutationKey;
export declare function buildCreateCustomerSessionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateCustomerSessionMutationVariables) => Promise<CreateCustomerSessionMutationData>;
};
//# sourceMappingURL=createCustomerSession.d.ts.map