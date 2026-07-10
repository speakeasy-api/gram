import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateGlobalRemoteSessionClientRequest, UpdateGlobalRemoteSessionClientSecurity } from "../models/operations/updateglobalremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateGlobalRemoteSessionClientMutationVariables = {
    request: UpdateGlobalRemoteSessionClientRequest;
    security?: UpdateGlobalRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateGlobalRemoteSessionClientMutationData = RemoteSessionClient;
export type UpdateGlobalRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateGlobalClient adminRemoteSessions
 *
 * @remarks
 * Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.
 */
export declare function useUpdateGlobalRemoteSessionClientMutation(options?: MutationHookOptions<UpdateGlobalRemoteSessionClientMutationData, UpdateGlobalRemoteSessionClientMutationError, UpdateGlobalRemoteSessionClientMutationVariables>): UseMutationResult<UpdateGlobalRemoteSessionClientMutationData, UpdateGlobalRemoteSessionClientMutationError, UpdateGlobalRemoteSessionClientMutationVariables>;
export declare function mutationKeyUpdateGlobalRemoteSessionClient(): MutationKey;
export declare function buildUpdateGlobalRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateGlobalRemoteSessionClientMutationVariables) => Promise<UpdateGlobalRemoteSessionClientMutationData>;
};
//# sourceMappingURL=updateGlobalRemoteSessionClient.d.ts.map