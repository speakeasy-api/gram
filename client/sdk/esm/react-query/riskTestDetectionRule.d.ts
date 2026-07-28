import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TestDetectionRuleResult } from "../models/components/testdetectionruleresult.js";
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
  TestDetectionRuleRequest,
  TestDetectionRuleSecurity,
} from "../models/operations/testdetectionrule.js";
import { MutationHookOptions } from "./_types.js";
export type RiskTestDetectionRuleMutationVariables = {
  request: TestDetectionRuleRequest;
  security?: TestDetectionRuleSecurity | undefined;
  options?: RequestOptions;
};
export type RiskTestDetectionRuleMutationData = TestDetectionRuleResult;
export type RiskTestDetectionRuleMutationError =
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
 * testDetectionRule risk
 *
 * @remarks
 * Run a single detection rule against pasted sample text and return any matches. Reuses the same scanner code (gitleaks, Presidio, prompt-injection, custom regex) that the analyzer runs in production so the playground match shape mirrors the chat-message path.
 */
export declare function useRiskTestDetectionRuleMutation(
  options?: MutationHookOptions<
    RiskTestDetectionRuleMutationData,
    RiskTestDetectionRuleMutationError,
    RiskTestDetectionRuleMutationVariables
  >,
): UseMutationResult<
  RiskTestDetectionRuleMutationData,
  RiskTestDetectionRuleMutationError,
  RiskTestDetectionRuleMutationVariables
>;
export declare function mutationKeyRiskTestDetectionRule(): MutationKey;
export declare function buildRiskTestDetectionRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskTestDetectionRuleMutationVariables,
  ) => Promise<RiskTestDetectionRuleMutationData>;
};
//# sourceMappingURL=riskTestDetectionRule.d.ts.map
