import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AddExternalOAuthServerRequest, AddExternalOAuthServerSecurity } from "../models/operations/addexternaloauthserver.js";
import { MutationHookOptions } from "./_types.js";
export type AddExternalOAuthServerMutationVariables = {
    request: AddExternalOAuthServerRequest;
    security?: AddExternalOAuthServerSecurity | undefined;
    options?: RequestOptions;
};
export type AddExternalOAuthServerMutationData = Toolset;
export type AddExternalOAuthServerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * addExternalOAuthServer toolsets
 *
 * @remarks
 * Associate an external OAuth server with a toolset
 */
export declare function useAddExternalOAuthServerMutation(options?: MutationHookOptions<AddExternalOAuthServerMutationData, AddExternalOAuthServerMutationError, AddExternalOAuthServerMutationVariables>): UseMutationResult<AddExternalOAuthServerMutationData, AddExternalOAuthServerMutationError, AddExternalOAuthServerMutationVariables>;
export declare function mutationKeyAddExternalOAuthServer(): MutationKey;
export declare function buildAddExternalOAuthServerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AddExternalOAuthServerMutationVariables) => Promise<AddExternalOAuthServerMutationData>;
};
//# sourceMappingURL=addExternalOAuthServer.d.ts.map