import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskExclusion } from "../models/components/riskexclusion.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateRiskExclusionRequest, UpdateRiskExclusionSecurity } from "../models/operations/updateriskexclusion.js";
import { MutationHookOptions } from "./_types.js";
export type RiskUpdateExclusionMutationVariables = {
    request: UpdateRiskExclusionRequest;
    security?: UpdateRiskExclusionSecurity | undefined;
    options?: RequestOptions;
};
export type RiskUpdateExclusionMutationData = RiskExclusion;
export type RiskUpdateExclusionMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * updateRiskExclusion risk
 *
 * @remarks
 * Update a risk exclusion.
 */
export declare function useRiskUpdateExclusionMutation(options?: MutationHookOptions<RiskUpdateExclusionMutationData, RiskUpdateExclusionMutationError, RiskUpdateExclusionMutationVariables>): UseMutationResult<RiskUpdateExclusionMutationData, RiskUpdateExclusionMutationError, RiskUpdateExclusionMutationVariables>;
export declare function mutationKeyRiskUpdateExclusion(): MutationKey;
export declare function buildRiskUpdateExclusionMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskUpdateExclusionMutationVariables) => Promise<RiskUpdateExclusionMutationData>;
};
//# sourceMappingURL=riskUpdateExclusion.d.ts.map