import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationInvitation } from "../models/components/organizationinvitation.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SendInviteRequest, SendInviteSecurity } from "../models/operations/sendinvite.js";
import { MutationHookOptions } from "./_types.js";
export type SendInviteMutationVariables = {
    request: SendInviteRequest;
    security?: SendInviteSecurity | undefined;
    options?: RequestOptions;
};
export type SendInviteMutationData = OrganizationInvitation;
export type SendInviteMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * sendInvite organizations
 *
 * @remarks
 * Send a WorkOS invitation for the active organization.
 */
export declare function useSendInviteMutation(options?: MutationHookOptions<SendInviteMutationData, SendInviteMutationError, SendInviteMutationVariables>): UseMutationResult<SendInviteMutationData, SendInviteMutationError, SendInviteMutationVariables>;
export declare function mutationKeySendInvite(): MutationKey;
export declare function buildSendInviteMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: SendInviteMutationVariables) => Promise<SendInviteMutationData>;
};
//# sourceMappingURL=sendInvite.d.ts.map