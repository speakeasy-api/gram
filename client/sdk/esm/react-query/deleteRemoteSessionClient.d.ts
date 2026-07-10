import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteRemoteSessionClientRequest, DeleteRemoteSessionClientSecurity } from "../models/operations/deleteremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteRemoteSessionClientMutationVariables = {
    request: DeleteRemoteSessionClientRequest;
    security?: DeleteRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteRemoteSessionClientMutationData = void;
export type DeleteRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Soft-delete a remote_session_client. Cascades to remote_sessions rows pointing at this client; affected principals are forced to re-authenticate.
 */
export declare function useDeleteRemoteSessionClientMutation(options?: MutationHookOptions<DeleteRemoteSessionClientMutationData, DeleteRemoteSessionClientMutationError, DeleteRemoteSessionClientMutationVariables>): UseMutationResult<DeleteRemoteSessionClientMutationData, DeleteRemoteSessionClientMutationError, DeleteRemoteSessionClientMutationVariables>;
export declare function mutationKeyDeleteRemoteSessionClient(): MutationKey;
export declare function buildDeleteRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteRemoteSessionClientMutationVariables) => Promise<DeleteRemoteSessionClientMutationData>;
};
//# sourceMappingURL=deleteRemoteSessionClient.d.ts.map