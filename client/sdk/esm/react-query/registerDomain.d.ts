import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RegisterDomainRequest, RegisterDomainSecurity } from "../models/operations/registerdomain.js";
import { MutationHookOptions } from "./_types.js";
export type RegisterDomainMutationVariables = {
    request: RegisterDomainRequest;
    security?: RegisterDomainSecurity | undefined;
    options?: RequestOptions;
};
export type RegisterDomainMutationData = void;
export type RegisterDomainMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createDomain domains
 *
 * @remarks
 * Create a custom domain for an organization
 */
export declare function useRegisterDomainMutation(options?: MutationHookOptions<RegisterDomainMutationData, RegisterDomainMutationError, RegisterDomainMutationVariables>): UseMutationResult<RegisterDomainMutationData, RegisterDomainMutationError, RegisterDomainMutationVariables>;
export declare function mutationKeyRegisterDomain(): MutationKey;
export declare function buildRegisterDomainMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RegisterDomainMutationVariables) => Promise<RegisterDomainMutationData>;
};
//# sourceMappingURL=registerDomain.d.ts.map