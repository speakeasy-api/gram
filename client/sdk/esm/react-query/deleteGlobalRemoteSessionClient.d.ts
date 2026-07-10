import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteGlobalRemoteSessionClientRequest, DeleteGlobalRemoteSessionClientSecurity } from "../models/operations/deleteglobalremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteGlobalRemoteSessionClientMutationVariables = {
    request: DeleteGlobalRemoteSessionClientRequest;
    security?: DeleteGlobalRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteGlobalRemoteSessionClientMutationData = void;
export type DeleteGlobalRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteGlobalClient adminRemoteSessions
 *
 * @remarks
 * Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.
 */
export declare function useDeleteGlobalRemoteSessionClientMutation(options?: MutationHookOptions<DeleteGlobalRemoteSessionClientMutationData, DeleteGlobalRemoteSessionClientMutationError, DeleteGlobalRemoteSessionClientMutationVariables>): UseMutationResult<DeleteGlobalRemoteSessionClientMutationData, DeleteGlobalRemoteSessionClientMutationError, DeleteGlobalRemoteSessionClientMutationVariables>;
export declare function mutationKeyDeleteGlobalRemoteSessionClient(): MutationKey;
export declare function buildDeleteGlobalRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteGlobalRemoteSessionClientMutationVariables) => Promise<DeleteGlobalRemoteSessionClientMutationData>;
};
//# sourceMappingURL=deleteGlobalRemoteSessionClient.d.ts.map