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
  DeleteCustomDetectionRuleRequest,
  DeleteCustomDetectionRuleSecurity,
} from "../models/operations/deletecustomdetectionrule.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDeleteCustomDetectionRuleMutationVariables = {
  request: DeleteCustomDetectionRuleRequest;
  security?: DeleteCustomDetectionRuleSecurity | undefined;
  options?: RequestOptions;
};
export type RiskDeleteCustomDetectionRuleMutationData = void;
export type RiskDeleteCustomDetectionRuleMutationError =
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
 * deleteCustomDetectionRule risk
 *
 * @remarks
 * Delete a custom detection rule.
 */
export declare function useRiskDeleteCustomDetectionRuleMutation(
  options?: MutationHookOptions<
    RiskDeleteCustomDetectionRuleMutationData,
    RiskDeleteCustomDetectionRuleMutationError,
    RiskDeleteCustomDetectionRuleMutationVariables
  >,
): UseMutationResult<
  RiskDeleteCustomDetectionRuleMutationData,
  RiskDeleteCustomDetectionRuleMutationError,
  RiskDeleteCustomDetectionRuleMutationVariables
>;
export declare function mutationKeyRiskDeleteCustomDetectionRule(): MutationKey;
export declare function buildRiskDeleteCustomDetectionRuleMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskDeleteCustomDetectionRuleMutationVariables,
  ) => Promise<RiskDeleteCustomDetectionRuleMutationData>;
};
//# sourceMappingURL=riskDeleteCustomDetectionRule.d.ts.map
