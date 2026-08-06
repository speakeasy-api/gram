import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type RiskUpdatePolicyMutationVariables = {
    request: operations.UpdateRiskPolicyRequest;
    security?: operations.UpdateRiskPolicySecurity | undefined;
    options?: RequestOptions;
};
export type RiskUpdatePolicyMutationData = components.RiskPolicy;
export type RiskUpdatePolicyMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateRiskPolicy risk
 *
 * @remarks
 * Update a risk analysis policy.
 */
export declare function useRiskUpdatePolicyMutation(options?: MutationHookOptions<RiskUpdatePolicyMutationData, RiskUpdatePolicyMutationError, RiskUpdatePolicyMutationVariables>): UseMutationResult<RiskUpdatePolicyMutationData, RiskUpdatePolicyMutationError, RiskUpdatePolicyMutationVariables>;
export declare function mutationKeyRiskUpdatePolicy(): MutationKey;
export declare function buildRiskUpdatePolicyMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskUpdatePolicyMutationVariables) => Promise<RiskUpdatePolicyMutationData>;
};
//# sourceMappingURL=riskUpdatePolicy.d.ts.map