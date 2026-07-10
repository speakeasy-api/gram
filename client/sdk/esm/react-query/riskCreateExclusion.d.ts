import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskExclusion } from "../models/components/riskexclusion.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateRiskExclusionRequest, CreateRiskExclusionSecurity } from "../models/operations/createriskexclusion.js";
import { MutationHookOptions } from "./_types.js";
export type RiskCreateExclusionMutationVariables = {
    request: CreateRiskExclusionRequest;
    security?: CreateRiskExclusionSecurity | undefined;
    options?: RequestOptions;
};
export type RiskCreateExclusionMutationData = RiskExclusion;
export type RiskCreateExclusionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createRiskExclusion risk
 *
 * @remarks
 * Create a risk exclusion. Omit risk_policy_id to create a global exclusion that applies to every policy in the project.
 */
export declare function useRiskCreateExclusionMutation(options?: MutationHookOptions<RiskCreateExclusionMutationData, RiskCreateExclusionMutationError, RiskCreateExclusionMutationVariables>): UseMutationResult<RiskCreateExclusionMutationData, RiskCreateExclusionMutationError, RiskCreateExclusionMutationVariables>;
export declare function mutationKeyRiskCreateExclusion(): MutationKey;
export declare function buildRiskCreateExclusionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskCreateExclusionMutationVariables) => Promise<RiskCreateExclusionMutationData>;
};
//# sourceMappingURL=riskCreateExclusion.d.ts.map