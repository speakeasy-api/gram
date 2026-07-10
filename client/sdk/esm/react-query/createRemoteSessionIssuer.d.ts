import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRemoteSessionIssuerRequest, CreateRemoteSessionIssuerSecurity } from "../models/operations/createremotesessionissuer.js";
import { MutationHookOptions } from "./_types.js";
export type CreateRemoteSessionIssuerMutationVariables = {
    request: CreateRemoteSessionIssuerRequest;
    security?: CreateRemoteSessionIssuerSecurity | undefined;
    options?: RequestOptions;
};
export type CreateRemoteSessionIssuerMutationData = RemoteSessionIssuer;
export type CreateRemoteSessionIssuerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRemoteSessionIssuer remoteSessionIssuers
 *
 * @remarks
 * Create a new remote_session_issuer.
 */
export declare function useCreateRemoteSessionIssuerMutation(options?: MutationHookOptions<CreateRemoteSessionIssuerMutationData, CreateRemoteSessionIssuerMutationError, CreateRemoteSessionIssuerMutationVariables>): UseMutationResult<CreateRemoteSessionIssuerMutationData, CreateRemoteSessionIssuerMutationError, CreateRemoteSessionIssuerMutationVariables>;
export declare function mutationKeyCreateRemoteSessionIssuer(): MutationKey;
export declare function buildCreateRemoteSessionIssuerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateRemoteSessionIssuerMutationVariables) => Promise<CreateRemoteSessionIssuerMutationData>;
};
//# sourceMappingURL=createRemoteSessionIssuer.d.ts.map