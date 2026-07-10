import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateCimdRemoteSessionClientRequest, CreateCimdRemoteSessionClientSecurity } from "../models/operations/createcimdremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type CreateCimdRemoteSessionClientMutationVariables = {
    request: CreateCimdRemoteSessionClientRequest;
    security?: CreateCimdRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type CreateCimdRemoteSessionClientMutationData = RemoteSessionClient;
export type CreateCimdRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createCimd remoteSessionClients
 *
 * @remarks
 * Register a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the client carries no secret and authenticates with token_endpoint_auth_method=none. The owning issuer must advertise client_id_metadata_document_supported.
 */
export declare function useCreateCimdRemoteSessionClientMutation(options?: MutationHookOptions<CreateCimdRemoteSessionClientMutationData, CreateCimdRemoteSessionClientMutationError, CreateCimdRemoteSessionClientMutationVariables>): UseMutationResult<CreateCimdRemoteSessionClientMutationData, CreateCimdRemoteSessionClientMutationError, CreateCimdRemoteSessionClientMutationVariables>;
export declare function mutationKeyCreateCimdRemoteSessionClient(): MutationKey;
export declare function buildCreateCimdRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateCimdRemoteSessionClientMutationVariables) => Promise<CreateCimdRemoteSessionClientMutationData>;
};
//# sourceMappingURL=createCimdRemoteSessionClient.d.ts.map