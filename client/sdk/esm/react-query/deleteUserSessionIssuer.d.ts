import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteUserSessionIssuerRequest, DeleteUserSessionIssuerSecurity } from "../models/operations/deleteusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteUserSessionIssuerMutationVariables = {
    request: DeleteUserSessionIssuerRequest;
    security?: DeleteUserSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteUserSessionIssuerMutationData = void;
export type DeleteUserSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteUserSessionIssuer userSessionIssuers
 *
 * @remarks
 * Soft-delete a user_session_issuer. Cascades to dependent user_sessions, user_session_consents, and remote_session_clients.
 */
export declare function useDeleteUserSessionIssuerMutation(options?: MutationHookOptions<DeleteUserSessionIssuerMutationData, DeleteUserSessionIssuerMutationError, DeleteUserSessionIssuerMutationVariables>): UseMutationResult<DeleteUserSessionIssuerMutationData, DeleteUserSessionIssuerMutationError, DeleteUserSessionIssuerMutationVariables>;
export declare function mutationKeyDeleteUserSessionIssuer(): MutationKey;
export declare function buildDeleteUserSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteUserSessionIssuerMutationVariables) => Promise<DeleteUserSessionIssuerMutationData>;
};
//# sourceMappingURL=deleteUserSessionIssuer.d.ts.map