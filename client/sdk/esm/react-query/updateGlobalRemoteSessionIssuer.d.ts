import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateGlobalRemoteSessionIssuerRequest, UpdateGlobalRemoteSessionIssuerSecurity } from "../models/operations/updateglobalremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateGlobalRemoteSessionIssuerMutationVariables = {
    request: UpdateGlobalRemoteSessionIssuerRequest;
    security?: UpdateGlobalRemoteSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type UpdateGlobalRemoteSessionIssuerMutationData = RemoteSessionIssuer;
export type UpdateGlobalRemoteSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateGlobalIssuer adminRemoteSessions
 *
 * @remarks
 * Update a global remote_session_issuer. Requires platform admin.
 */
export declare function useUpdateGlobalRemoteSessionIssuerMutation(options?: MutationHookOptions<UpdateGlobalRemoteSessionIssuerMutationData, UpdateGlobalRemoteSessionIssuerMutationError, UpdateGlobalRemoteSessionIssuerMutationVariables>): UseMutationResult<UpdateGlobalRemoteSessionIssuerMutationData, UpdateGlobalRemoteSessionIssuerMutationError, UpdateGlobalRemoteSessionIssuerMutationVariables>;
export declare function mutationKeyUpdateGlobalRemoteSessionIssuer(): MutationKey;
export declare function buildUpdateGlobalRemoteSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: UpdateGlobalRemoteSessionIssuerMutationVariables) => Promise<UpdateGlobalRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=updateGlobalRemoteSessionIssuer.d.ts.map