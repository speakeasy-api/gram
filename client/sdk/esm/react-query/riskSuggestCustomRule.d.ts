import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SuggestCustomDetectionRuleResult } from "../models/components/suggestcustomdetectionruleresult.js";
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
  SuggestCustomDetectionRuleRequest,
  SuggestCustomDetectionRuleSecurity,
} from "../models/operations/suggestcustomdetectionrule.js";
import { MutationHookOptions } from "./_types.js";
export type RiskSuggestCustomRuleMutationVariables = {
  request: SuggestCustomDetectionRuleRequest;
  security?: SuggestCustomDetectionRuleSecurity | undefined;
  options?: RequestOptions;
};
export type RiskSuggestCustomRuleMutationData =
  SuggestCustomDetectionRuleResult;
export type RiskSuggestCustomRuleMutationError =
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
 * suggestCustomDetectionRule risk
 *
 * @remarks
 * Suggest a custom detection rule (rule_id, title, description, regex, severity) from a natural-language prompt. Calls the configured LLM with a JSON-schema constrained response so the dashboard can prefill the create form.
 */
export declare function useRiskSuggestCustomRuleMutation(
  options?: MutationHookOptions<
    RiskSuggestCustomRuleMutationData,
    RiskSuggestCustomRuleMutationError,
    RiskSuggestCustomRuleMutationVariables
  >,
): UseMutationResult<
  RiskSuggestCustomRuleMutationData,
  RiskSuggestCustomRuleMutationError,
  RiskSuggestCustomRuleMutationVariables
>;
export declare function mutationKeyRiskSuggestCustomRule(): MutationKey;
export declare function buildRiskSuggestCustomRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskSuggestCustomRuleMutationVariables,
  ) => Promise<RiskSuggestCustomRuleMutationData>;
};
//# sourceMappingURL=riskSuggestCustomRule.d.ts.map
