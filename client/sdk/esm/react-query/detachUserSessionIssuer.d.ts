import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DetachUserSessionIssuerRequest, DetachUserSessionIssuerSecurity } from "../models/operations/detachusersessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type DetachUserSessionIssuerMutationVariables = {
    request: DetachUserSessionIssuerRequest;
    security?: DetachUserSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type DetachUserSessionIssuerMutationData = RemoteSessionClient;
export type DetachUserSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * detachUserSessionIssuer remoteSessionClients
 *
 * @remarks
 * Detach a user_session_issuer from a remote_session_client by removing the binding from the join table. A no-op when the binding does not exist.
 */
export declare function useDetachUserSessionIssuerMutation(options?: MutationHookOptions<DetachUserSessionIssuerMutationData, DetachUserSessionIssuerMutationError, DetachUserSessionIssuerMutationVariables>): UseMutationResult<DetachUserSessionIssuerMutationData, DetachUserSessionIssuerMutationError, DetachUserSessionIssuerMutationVariables>;
export declare function mutationKeyDetachUserSessionIssuer(): MutationKey;
export declare function buildDetachUserSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DetachUserSessionIssuerMutationVariables) => Promise<DetachUserSessionIssuerMutationData>;
};
//# sourceMappingURL=detachUserSessionIssuer.d.ts.map