import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ShadowMCPAccessRule } from "../models/components/shadowmcpaccessrule.js";
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
  UpdateShadowMCPAccessRuleRequest,
  UpdateShadowMCPAccessRuleSecurity,
} from "../models/operations/updateshadowmcpaccessrule.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateShadowMCPAccessRuleMutationVariables = {
  request: UpdateShadowMCPAccessRuleRequest;
  security?: UpdateShadowMCPAccessRuleSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateShadowMCPAccessRuleMutationData = ShadowMCPAccessRule;
export type UpdateShadowMCPAccessRuleMutationError =
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
 * updateShadowMCPAccessRule access
 *
 * @remarks
 * Update a managed Shadow MCP access rule.
 */
export declare function useUpdateShadowMCPAccessRuleMutation(
  options?: MutationHookOptions<
    UpdateShadowMCPAccessRuleMutationData,
    UpdateShadowMCPAccessRuleMutationError,
    UpdateShadowMCPAccessRuleMutationVariables
  >,
): UseMutationResult<
  UpdateShadowMCPAccessRuleMutationData,
  UpdateShadowMCPAccessRuleMutationError,
  UpdateShadowMCPAccessRuleMutationVariables
>;
export declare function mutationKeyUpdateShadowMCPAccessRule(): MutationKey;
export declare function buildUpdateShadowMCPAccessRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateShadowMCPAccessRuleMutationVariables,
  ) => Promise<UpdateShadowMCPAccessRuleMutationData>;
};
//# sourceMappingURL=updateShadowMCPAccessRule.d.ts.map
