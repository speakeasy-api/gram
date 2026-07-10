import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyBypassRequest } from "../models/components/riskpolicybypassrequest.js";
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
  ApproveRiskPolicyBypassRequestRequest,
  ApproveRiskPolicyBypassRequestSecurity,
} from "../models/operations/approveriskpolicybypassrequest.js";
import { MutationHookOptions } from "./_types.js";
export type RiskApprovePolicyBypassRequestMutationVariables = {
  request: ApproveRiskPolicyBypassRequestRequest;
  security?: ApproveRiskPolicyBypassRequestSecurity | undefined;
  options?: RequestOptions;
};
export type RiskApprovePolicyBypassRequestMutationData =
  RiskPolicyBypassRequest;
export type RiskApprovePolicyBypassRequestMutationError =
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
 * approveRiskPolicyBypassRequest risk
 *
 * @remarks
 * Approve a risk policy bypass request for the requested policy target.
 */
export declare function useRiskApprovePolicyBypassRequestMutation(
  options?: MutationHookOptions<
    RiskApprovePolicyBypassRequestMutationData,
    RiskApprovePolicyBypassRequestMutationError,
    RiskApprovePolicyBypassRequestMutationVariables
  >,
): UseMutationResult<
  RiskApprovePolicyBypassRequestMutationData,
  RiskApprovePolicyBypassRequestMutationError,
  RiskApprovePolicyBypassRequestMutationVariables
>;
export declare function mutationKeyRiskApprovePolicyBypassRequest(): MutationKey;
export declare function buildRiskApprovePolicyBypassRequestMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskApprovePolicyBypassRequestMutationVariables,
  ) => Promise<RiskApprovePolicyBypassRequestMutationData>;
};
//# sourceMappingURL=riskApprovePolicyBypassRequest.d.ts.map
