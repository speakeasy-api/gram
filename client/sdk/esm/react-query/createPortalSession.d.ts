import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePortalSessionResult } from "../models/components/createportalsessionresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreatePortalSessionRequest, CreatePortalSessionSecurity } from "../models/operations/createportalsession.js";
import { MutationHookOptions } from "./_types.js";
export type CreatePortalSessionMutationVariables = {
    request?: CreatePortalSessionRequest | undefined;
    security?: CreatePortalSessionSecurity | undefined;
    options?: RequestOptions;
};
export type CreatePortalSessionMutationData = CreatePortalSessionResult;
export type CreatePortalSessionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createPortalSession organizations
 *
 * @remarks
 * Create a webhook portal session.
 */
export declare function useCreatePortalSessionMutation(options?: MutationHookOptions<CreatePortalSessionMutationData, CreatePortalSessionMutationError, CreatePortalSessionMutationVariables>): UseMutationResult<CreatePortalSessionMutationData, CreatePortalSessionMutationError, CreatePortalSessionMutationVariables>;
export declare function mutationKeyCreatePortalSession(): MutationKey;
export declare function buildCreatePortalSessionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreatePortalSessionMutationVariables) => Promise<CreatePortalSessionMutationData>;
};
//# sourceMappingURL=createPortalSession.d.ts.map