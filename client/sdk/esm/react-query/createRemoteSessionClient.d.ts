import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteSessionClientRequest, CreateRemoteSessionClientSecurity } from "../models/operations/createremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type CreateRemoteSessionClientMutationVariables = {
    request: CreateRemoteSessionClientRequest;
    security?: CreateRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type CreateRemoteSessionClientMutationData = RemoteSessionClient;
export type CreateRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRemoteSessionClient remoteSessionClients
 *
 * @remarks
 * Register a remote_session_client by supplying a client_id and optional client_secret obtained out-of-band from the upstream issuer.
 */
export declare function useCreateRemoteSessionClientMutation(options?: MutationHookOptions<CreateRemoteSessionClientMutationData, CreateRemoteSessionClientMutationError, CreateRemoteSessionClientMutationVariables>): UseMutationResult<CreateRemoteSessionClientMutationData, CreateRemoteSessionClientMutationError, CreateRemoteSessionClientMutationVariables>;
export declare function mutationKeyCreateRemoteSessionClient(): MutationKey;
export declare function buildCreateRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateRemoteSessionClientMutationVariables) => Promise<CreateRemoteSessionClientMutationData>;
};
//# sourceMappingURL=createRemoteSessionClient.d.ts.map