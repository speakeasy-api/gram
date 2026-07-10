import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateOrganizationRemoteSessionClientRequest, UpdateOrganizationRemoteSessionClientSecurity } from "../models/operations/updateorganizationremotesessionclient.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateOrganizationRemoteSessionClientMutationVariables = {
    request: UpdateOrganizationRemoteSessionClientRequest;
    security?: UpdateOrganizationRemoteSessionClientSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateOrganizationRemoteSessionClientMutationData = RemoteSessionClient;
export type UpdateOrganizationRemoteSessionClientMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateClient organizationRemoteSessionClients
 *
 * @remarks
 * Update a remote_session_client's non-secret fields in the caller's organization. Requires org:admin.
 */
export declare function useUpdateOrganizationRemoteSessionClientMutation(options?: MutationHookOptions<UpdateOrganizationRemoteSessionClientMutationData, UpdateOrganizationRemoteSessionClientMutationError, UpdateOrganizationRemoteSessionClientMutationVariables>): UseMutationResult<UpdateOrganizationRemoteSessionClientMutationData, UpdateOrganizationRemoteSessionClientMutationError, UpdateOrganizationRemoteSessionClientMutationVariables>;
export declare function mutationKeyUpdateOrganizationRemoteSessionClient(): MutationKey;
export declare function buildUpdateOrganizationRemoteSessionClientMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateOrganizationRemoteSessionClientMutationVariables) => Promise<UpdateOrganizationRemoteSessionClientMutationData>;
};
//# sourceMappingURL=updateOrganizationRemoteSessionClient.d.ts.map