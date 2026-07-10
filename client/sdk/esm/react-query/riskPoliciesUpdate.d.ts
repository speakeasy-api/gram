import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
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
  UpdateRiskPolicyRequest,
  UpdateRiskPolicySecurity,
} from "../models/operations/updateriskpolicy.js";
import { MutationHookOptions } from "./_types.js";
export type RiskPoliciesUpdateMutationVariables = {
  request: UpdateRiskPolicyRequest;
  security?: UpdateRiskPolicySecurity | undefined;
  options?: RequestOptions;
};
export type RiskPoliciesUpdateMutationData = RiskPolicy;
export type RiskPoliciesUpdateMutationError =
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
 * updateRiskPolicy risk
 *
 * @remarks
 * Update a risk analysis policy.
 */
export declare function useRiskPoliciesUpdateMutation(
  options?: MutationHookOptions<
    RiskPoliciesUpdateMutationData,
    RiskPoliciesUpdateMutationError,
    RiskPoliciesUpdateMutationVariables
  >,
): UseMutationResult<
  RiskPoliciesUpdateMutationData,
  RiskPoliciesUpdateMutationError,
  RiskPoliciesUpdateMutationVariables
>;
export declare function mutationKeyRiskPoliciesUpdate(): MutationKey;
export declare function buildRiskPoliciesUpdateMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskPoliciesUpdateMutationVariables,
  ) => Promise<RiskPoliciesUpdateMutationData>;
};
//# sourceMappingURL=riskPoliciesUpdate.d.ts.map
