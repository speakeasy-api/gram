import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteTemplateRequest, DeleteTemplateSecurity } from "../models/operations/deletetemplate.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteTemplateMutationVariables = {
    request?: DeleteTemplateRequest | undefined;
    security?: DeleteTemplateSecurity | undefined;
    options?: RequestOptions;
};
export type DeleteTemplateMutationData = void;
export type DeleteTemplateMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteTemplate templates
 *
 * @remarks
 * Delete prompt template by its ID or name.
 */
export declare function useDeleteTemplateMutation(options?: MutationHookOptions<DeleteTemplateMutationData, DeleteTemplateMutationError, DeleteTemplateMutationVariables>): UseMutationResult<DeleteTemplateMutationData, DeleteTemplateMutationError, DeleteTemplateMutationVariables>;
export declare function mutationKeyDeleteTemplate(): MutationKey;
export declare function buildDeleteTemplateMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: DeleteTemplateMutationVariables) => Promise<DeleteTemplateMutationData>;
};
//# sourceMappingURL=deleteTemplate.d.ts.map