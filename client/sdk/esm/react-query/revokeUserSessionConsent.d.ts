import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RevokeUserSessionConsentRequest, RevokeUserSessionConsentSecurity } from "../models/operations/revokeusersessionconsent.js";
import { MutationHookOptions } from "./_types.js";
export type RevokeUserSessionConsentMutationVariables = {
    request: RevokeUserSessionConsentRequest;
    security?: RevokeUserSessionConsentSecurity | undefined;
    options?: RequestOptions;
};
export type RevokeUserSessionConsentMutationData = void;
export type RevokeUserSessionConsentMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * revokeUserSessionConsent userSessionConsents
 *
 * @remarks
 * Withdraw consent. Subsequent authorization requests for matching (subject, user_session_client) pairs re-prompt.
 */
export declare function useRevokeUserSessionConsentMutation(options?: MutationHookOptions<RevokeUserSessionConsentMutationData, RevokeUserSessionConsentMutationError, RevokeUserSessionConsentMutationVariables>): UseMutationResult<RevokeUserSessionConsentMutationData, RevokeUserSessionConsentMutationError, RevokeUserSessionConsentMutationVariables>;
export declare function mutationKeyRevokeUserSessionConsent(): MutationKey;
export declare function buildRevokeUserSessionConsentMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RevokeUserSessionConsentMutationVariables) => Promise<RevokeUserSessionConsentMutationData>;
};
//# sourceMappingURL=revokeUserSessionConsent.d.ts.map