import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteSourceEnvironmentLinkRequest, DeleteSourceEnvironmentLinkSecurity } from "../models/operations/deletesourceenvironmentlink.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteSourceEnvironmentLinkMutationVariables = {
    request: DeleteSourceEnvironmentLinkRequest;
    security?: DeleteSourceEnvironmentLinkSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteSourceEnvironmentLinkMutationData = void;
export type DeleteSourceEnvironmentLinkMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteSourceEnvironmentLink environments
 *
 * @remarks
 * Delete a link between a source and an environment
 */
export declare function useDeleteSourceEnvironmentLinkMutation(options?: MutationHookOptions<DeleteSourceEnvironmentLinkMutationData, DeleteSourceEnvironmentLinkMutationError, DeleteSourceEnvironmentLinkMutationVariables>): UseMutationResult<DeleteSourceEnvironmentLinkMutationData, DeleteSourceEnvironmentLinkMutationError, DeleteSourceEnvironmentLinkMutationVariables>;
export declare function mutationKeyDeleteSourceEnvironmentLink(): MutationKey;
export declare function buildDeleteSourceEnvironmentLinkMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteSourceEnvironmentLinkMutationVariables) => Promise<DeleteSourceEnvironmentLinkMutationData>;
};
//# sourceMappingURL=deleteSourceEnvironmentLink.d.ts.map