import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AttachUserSessionIssuerRequest, AttachUserSessionIssuerSecurity } from "../models/operations/attachusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type AttachUserSessionIssuerMutationVariables = {
    request: AttachUserSessionIssuerRequest;
    security?: AttachUserSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type AttachUserSessionIssuerMutationData = RemoteSessionClient;
export type AttachUserSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * attachUserSessionIssuer remoteSessionClients
 *
 * @remarks
 * Attach a user_session_issuer to a remote_session_client by recording the binding in the join table. Rejected when another client is already bound to the same user_session_issuer for this client's remote_session_issuer.
 */
export declare function useAttachUserSessionIssuerMutation(options?: MutationHookOptions<AttachUserSessionIssuerMutationData, AttachUserSessionIssuerMutationError, AttachUserSessionIssuerMutationVariables>): UseMutationResult<AttachUserSessionIssuerMutationData, AttachUserSessionIssuerMutationError, AttachUserSessionIssuerMutationVariables>;
export declare function mutationKeyAttachUserSessionIssuer(): MutationKey;
export declare function buildAttachUserSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AttachUserSessionIssuerMutationVariables) => Promise<AttachUserSessionIssuerMutationData>;
};
//# sourceMappingURL=attachUserSessionIssuer.d.ts.map