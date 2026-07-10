import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSession } from "../models/components/remotesession.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RefreshOrganizationRemoteSessionRequest, RefreshOrganizationRemoteSessionSecurity } from "../models/operations/refreshorganizationremotesession.js";
import { MutationHookOptions } from "./_types.js";
export type RefreshOrganizationRemoteSessionMutationVariables = {
    request: RefreshOrganizationRemoteSessionRequest;
    security?: RefreshOrganizationRemoteSessionSecurity | undefined;
    options?: RequestOptions;
};
export type RefreshOrganizationRemoteSessionMutationData = RemoteSession;
export type RefreshOrganizationRemoteSessionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * refreshSession organizationRemoteSessions
 *
 * @remarks
 * Force an upstream token refresh on a single remote_session in the caller's organization, regardless of current access-token expiry. Returns the updated remote_session so callers can reflect the new expiry without a refetch. Fails with a bad-request error when the session holds no refresh token. Requires org:admin.
 */
export declare function useRefreshOrganizationRemoteSessionMutation(options?: MutationHookOptions<RefreshOrganizationRemoteSessionMutationData, RefreshOrganizationRemoteSessionMutationError, RefreshOrganizationRemoteSessionMutationVariables>): UseMutationResult<RefreshOrganizationRemoteSessionMutationData, RefreshOrganizationRemoteSessionMutationError, RefreshOrganizationRemoteSessionMutationVariables>;
export declare function mutationKeyRefreshOrganizationRemoteSession(): MutationKey;
export declare function buildRefreshOrganizationRemoteSessionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RefreshOrganizationRemoteSessionMutationVariables) => Promise<RefreshOrganizationRemoteSessionMutationData>;
};
//# sourceMappingURL=refreshOrganizationRemoteSession.d.ts.map