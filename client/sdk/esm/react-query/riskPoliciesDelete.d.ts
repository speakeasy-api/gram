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
  DeleteRiskPolicyRequest,
  DeleteRiskPolicySecurity,
} from "../models/operations/deleteriskpolicy.js";
import { MutationHookOptions } from "./_types.js";
export type RiskPoliciesDeleteMutationVariables = {
  request: DeleteRiskPolicyRequest;
  security?: DeleteRiskPolicySecurity | undefined;
  options?: RequestOptions;
};
export type RiskPoliciesDeleteMutationData = void;
export type RiskPoliciesDeleteMutationError =
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
 * deleteRiskPolicy risk
 *
 * @remarks
 * Delete a risk analysis policy.
 */
export declare function useRiskPoliciesDeleteMutation(
  options?: MutationHookOptions<
    RiskPoliciesDeleteMutationData,
    RiskPoliciesDeleteMutationError,
    RiskPoliciesDeleteMutationVariables
  >,
): UseMutationResult<
  RiskPoliciesDeleteMutationData,
  RiskPoliciesDeleteMutationError,
  RiskPoliciesDeleteMutationVariables
>;
export declare function mutationKeyRiskPoliciesDelete(): MutationKey;
export declare function buildRiskPoliciesDeleteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskPoliciesDeleteMutationVariables,
  ) => Promise<RiskPoliciesDeleteMutationData>;
};
//# sourceMappingURL=riskPoliciesDelete.d.ts.map
