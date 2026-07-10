import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeUserSessionClientRequest, RevokeUserSessionClientSecurity } from "../models/operations/revokeusersessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeUserSessionClientMutationVariables = {
    request: RevokeUserSessionClientRequest;
    security?: RevokeUserSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type RevokeUserSessionClientMutationData = void;
export type RevokeUserSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * revokeUserSessionClient userSessionClients
 *
 * @remarks
 * Soft-delete a user_session_client. Future tokens minted for this client_id are rejected; existing live user_sessions keep working until they hit expires_at.
 */
export declare function useRevokeUserSessionClientMutation(options?: MutationHookOptions<RevokeUserSessionClientMutationData, RevokeUserSessionClientMutationError, RevokeUserSessionClientMutationVariables>): UseMutationResult<RevokeUserSessionClientMutationData, RevokeUserSessionClientMutationError, RevokeUserSessionClientMutationVariables>;
export declare function mutationKeyRevokeUserSessionClient(): MutationKey;
export declare function buildRevokeUserSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RevokeUserSessionClientMutationVariables) => Promise<RevokeUserSessionClientMutationData>;
};
//# sourceMappingURL=revokeUserSessionClient.d.ts.map