import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateGlobalRemoteSessionClientRequest, CreateGlobalRemoteSessionClientSecurity } from "../models/operations/createglobalremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type CreateGlobalRemoteSessionClientMutationVariables = {
    request: CreateGlobalRemoteSessionClientRequest;
    security?: CreateGlobalRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type CreateGlobalRemoteSessionClientMutationData = RemoteSessionClient;
export type CreateGlobalRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createGlobalClient adminRemoteSessions
 *
 * @remarks
 * Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.
 */
export declare function useCreateGlobalRemoteSessionClientMutation(options?: MutationHookOptions<CreateGlobalRemoteSessionClientMutationData, CreateGlobalRemoteSessionClientMutationError, CreateGlobalRemoteSessionClientMutationVariables>): UseMutationResult<CreateGlobalRemoteSessionClientMutationData, CreateGlobalRemoteSessionClientMutationError, CreateGlobalRemoteSessionClientMutationVariables>;
export declare function mutationKeyCreateGlobalRemoteSessionClient(): MutationKey;
export declare function buildCreateGlobalRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateGlobalRemoteSessionClientMutationVariables) => Promise<CreateGlobalRemoteSessionClientMutationData>;
};
//# sourceMappingURL=createGlobalRemoteSessionClient.d.ts.map