import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpdatePromptTemplateResult } from "../models/components/updateprompttemplateresult.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  UpdateTemplateRequest,
  UpdateTemplateSecurity,
} from "../models/operations/updatetemplate.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateTemplateMutationVariables = {
  request: UpdateTemplateRequest;
  security?: UpdateTemplateSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateTemplateMutationData = UpdatePromptTemplateResult;
export type UpdateTemplateMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * updateTemplate templates
 *
 * @remarks
 * Update a prompt template.
 */
export declare function useUpdateTemplateMutation(
  options?: MutationHookOptions<
    UpdateTemplateMutationData,
    UpdateTemplateMutationError,
    UpdateTemplateMutationVariables
  >,
): UseMutationResult<
  UpdateTemplateMutationData,
  UpdateTemplateMutationError,
  UpdateTemplateMutationVariables
>;
export declare function mutationKeyUpdateTemplate(): MutationKey;
export declare function buildUpdateTemplateMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateTemplateMutationVariables,
  ) => Promise<UpdateTemplateMutationData>;
};
//# sourceMappingURL=updateTemplate.d.ts.map
