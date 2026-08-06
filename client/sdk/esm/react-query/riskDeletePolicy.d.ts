import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDeletePolicyMutationVariables = {
    request: operations.DeleteRiskPolicyRequest;
    security?: operations.DeleteRiskPolicySecurity | undefined;
    options?: RequestOptions;
};
export type RiskDeletePolicyMutationData = void;
export type RiskDeletePolicyMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * deleteRiskPolicy risk
 *
 * @remarks
 * Delete a risk analysis policy.
 */
export declare function useRiskDeletePolicyMutation(options?: MutationHookOptions<RiskDeletePolicyMutationData, RiskDeletePolicyMutationError, RiskDeletePolicyMutationVariables>): UseMutationResult<RiskDeletePolicyMutationData, RiskDeletePolicyMutationError, RiskDeletePolicyMutationVariables>;
export declare function mutationKeyRiskDeletePolicy(): MutationKey;
export declare function buildRiskDeletePolicyMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskDeletePolicyMutationVariables) => Promise<RiskDeletePolicyMutationData>;
};
//# sourceMappingURL=riskDeletePolicy.d.ts.map