import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteOrganizationRemoteSessionClientRequest, DeleteOrganizationRemoteSessionClientSecurity } from "../models/operations/deleteorganizationremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteOrganizationRemoteSessionClientMutationVariables = {
    request: DeleteOrganizationRemoteSessionClientRequest;
    security?: DeleteOrganizationRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteOrganizationRemoteSessionClientMutationData = void;
export type DeleteOrganizationRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteClient organizationRemoteSessionClients
 *
 * @remarks
 * Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.
 */
export declare function useDeleteOrganizationRemoteSessionClientMutation(options?: MutationHookOptions<DeleteOrganizationRemoteSessionClientMutationData, DeleteOrganizationRemoteSessionClientMutationError, DeleteOrganizationRemoteSessionClientMutationVariables>): UseMutationResult<DeleteOrganizationRemoteSessionClientMutationData, DeleteOrganizationRemoteSessionClientMutationError, DeleteOrganizationRemoteSessionClientMutationVariables>;
export declare function mutationKeyDeleteOrganizationRemoteSessionClient(): MutationKey;
export declare function buildDeleteOrganizationRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteOrganizationRemoteSessionClientMutationVariables) => Promise<DeleteOrganizationRemoteSessionClientMutationData>;
};
//# sourceMappingURL=deleteOrganizationRemoteSessionClient.d.ts.map