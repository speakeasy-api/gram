import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCustomDetectionRule } from "../models/components/riskcustomdetectionrule.js";
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
  UpdateCustomDetectionRuleRequest,
  UpdateCustomDetectionRuleSecurity,
} from "../models/operations/updatecustomdetectionrule.js";
import { MutationHookOptions } from "./_types.js";
export type RiskUpdateCustomDetectionRuleMutationVariables = {
  request: UpdateCustomDetectionRuleRequest;
  security?: UpdateCustomDetectionRuleSecurity | undefined;
  options?: RequestOptions;
};
export type RiskUpdateCustomDetectionRuleMutationData = RiskCustomDetectionRule;
export type RiskUpdateCustomDetectionRuleMutationError =
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
 * updateCustomDetectionRule risk
 *
 * @remarks
 * Update a custom detection rule.
 */
export declare function useRiskUpdateCustomDetectionRuleMutation(
  options?: MutationHookOptions<
    RiskUpdateCustomDetectionRuleMutationData,
    RiskUpdateCustomDetectionRuleMutationError,
    RiskUpdateCustomDetectionRuleMutationVariables
  >,
): UseMutationResult<
  RiskUpdateCustomDetectionRuleMutationData,
  RiskUpdateCustomDetectionRuleMutationError,
  RiskUpdateCustomDetectionRuleMutationVariables
>;
export declare function mutationKeyRiskUpdateCustomDetectionRule(): MutationKey;
export declare function buildRiskUpdateCustomDetectionRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskUpdateCustomDetectionRuleMutationVariables,
  ) => Promise<RiskUpdateCustomDetectionRuleMutationData>;
};
//# sourceMappingURL=riskUpdateCustomDetectionRule.d.ts.map
