import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateShadowMCPAccessRuleResult } from "../models/components/createshadowmcpaccessruleresult.js";
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
  CreateShadowMCPAccessRuleRequest,
  CreateShadowMCPAccessRuleSecurity,
} from "../models/operations/createshadowmcpaccessrule.js";
import { MutationHookOptions } from "./_types.js";
export type CreateShadowMCPAccessRuleMutationVariables = {
  request: CreateShadowMCPAccessRuleRequest;
  security?: CreateShadowMCPAccessRuleSecurity | undefined;
  options?: RequestOptions;
};
export type CreateShadowMCPAccessRuleMutationData =
  CreateShadowMCPAccessRuleResult;
export type CreateShadowMCPAccessRuleMutationError =
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
 * createShadowMCPAccessRule access
 *
 * @remarks
 * Create a managed Shadow MCP access rule.
 */
export declare function useCreateShadowMCPAccessRuleMutation(
  options?: MutationHookOptions<
    CreateShadowMCPAccessRuleMutationData,
    CreateShadowMCPAccessRuleMutationError,
    CreateShadowMCPAccessRuleMutationVariables
  >,
): UseMutationResult<
  CreateShadowMCPAccessRuleMutationData,
  CreateShadowMCPAccessRuleMutationError,
  CreateShadowMCPAccessRuleMutationVariables
>;
export declare function mutationKeyCreateShadowMCPAccessRule(): MutationKey;
export declare function buildCreateShadowMCPAccessRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateShadowMCPAccessRuleMutationVariables,
  ) => Promise<CreateShadowMCPAccessRuleMutationData>;
};
//# sourceMappingURL=createShadowMCPAccessRule.d.ts.map
