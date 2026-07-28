import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteShadowMCPAccessRuleRequest,
  DeleteShadowMCPAccessRuleSecurity,
} from "../models/operations/deleteshadowmcpaccessrule.js";
import { MutationHookOptions } from "./_types.js";
export type DeleteShadowMCPAccessRuleMutationVariables = {
  request: DeleteShadowMCPAccessRuleRequest;
  security?: DeleteShadowMCPAccessRuleSecurity | undefined;
  options?: RequestOptions;
};
export type DeleteShadowMCPAccessRuleMutationData = void;
export type DeleteShadowMCPAccessRuleMutationError =
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
 * deleteShadowMCPAccessRule access
 *
 * @remarks
 * Delete a managed Shadow MCP access rule.
 */
export declare function useDeleteShadowMCPAccessRuleMutation(
  options?: MutationHookOptions<
    DeleteShadowMCPAccessRuleMutationData,
    DeleteShadowMCPAccessRuleMutationError,
    DeleteShadowMCPAccessRuleMutationVariables
  >,
): UseMutationResult<
  DeleteShadowMCPAccessRuleMutationData,
  DeleteShadowMCPAccessRuleMutationError,
  DeleteShadowMCPAccessRuleMutationVariables
>;
export declare function mutationKeyDeleteShadowMCPAccessRule(): MutationKey;
export declare function buildDeleteShadowMCPAccessRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: DeleteShadowMCPAccessRuleMutationVariables,
  ) => Promise<DeleteShadowMCPAccessRuleMutationData>;
};
//# sourceMappingURL=deleteShadowMCPAccessRule.d.ts.map
