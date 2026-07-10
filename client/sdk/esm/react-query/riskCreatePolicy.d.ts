import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRiskPolicyRequest, CreateRiskPolicySecurity } from "../models/operations/createriskpolicy.js";
import { MutationHookOptions } from "./_types.js";
export type RiskCreatePolicyMutationVariables = {
    request: CreateRiskPolicyRequest;
    security?: CreateRiskPolicySecurity | undefined;
    options?: RequestOptions;
};
export type RiskCreatePolicyMutationData = RiskPolicy;
export type RiskCreatePolicyMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRiskPolicy risk
 *
 * @remarks
 * Create a new risk analysis policy for the current project.
 */
export declare function useRiskCreatePolicyMutation(options?: MutationHookOptions<RiskCreatePolicyMutationData, RiskCreatePolicyMutationError, RiskCreatePolicyMutationVariables>): UseMutationResult<RiskCreatePolicyMutationData, RiskCreatePolicyMutationError, RiskCreatePolicyMutationVariables>;
export declare function mutationKeyRiskCreatePolicy(): MutationKey;
export declare function buildRiskCreatePolicyMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskCreatePolicyMutationVariables) => Promise<RiskCreatePolicyMutationData>;
};
//# sourceMappingURL=riskCreatePolicy.d.ts.map