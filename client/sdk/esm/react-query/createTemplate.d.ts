import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePromptTemplateResult } from "../models/components/createprompttemplateresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateTemplateRequest, CreateTemplateSecurity } from "../models/operations/createtemplate.js";
import { MutationHookOptions } from "./_types.js";
export type CreateTemplateMutationVariables = {
    request: CreateTemplateRequest;
    security?: CreateTemplateSecurity | undefined;
    options?: RequestOptions;
};
export type CreateTemplateMutationData = CreatePromptTemplateResult;
export type CreateTemplateMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createTemplate templates
 *
 * @remarks
 * Create a new prompt template.
 */
export declare function useCreateTemplateMutation(options?: MutationHookOptions<CreateTemplateMutationData, CreateTemplateMutationError, CreateTemplateMutationVariables>): UseMutationResult<CreateTemplateMutationData, CreateTemplateMutationError, CreateTemplateMutationVariables>;
export declare function mutationKeyCreateTemplate(): MutationKey;
export declare function buildCreateTemplateMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: CreateTemplateMutationVariables) => Promise<CreateTemplateMutationData>;
};
//# sourceMappingURL=createTemplate.d.ts.map