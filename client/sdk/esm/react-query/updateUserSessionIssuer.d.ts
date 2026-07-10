import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionIssuer } from "../models/components/usersessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateUserSessionIssuerRequest, UpdateUserSessionIssuerSecurity } from "../models/operations/updateusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateUserSessionIssuerMutationVariables = {
    request: UpdateUserSessionIssuerRequest;
    security?: UpdateUserSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateUserSessionIssuerMutationData = UserSessionIssuer;
export type UpdateUserSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Update fields on an existing user_session_issuer.
 */
export declare function useUpdateUserSessionIssuerMutation(options?: MutationHookOptions<UpdateUserSessionIssuerMutationData, UpdateUserSessionIssuerMutationError, UpdateUserSessionIssuerMutationVariables>): UseMutationResult<UpdateUserSessionIssuerMutationData, UpdateUserSessionIssuerMutationError, UpdateUserSessionIssuerMutationVariables>;
export declare function mutationKeyUpdateUserSessionIssuer(): MutationKey;
export declare function buildUpdateUserSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateUserSessionIssuerMutationVariables) => Promise<UpdateUserSessionIssuerMutationData>;
};
//# sourceMappingURL=updateUserSessionIssuer.d.ts.map