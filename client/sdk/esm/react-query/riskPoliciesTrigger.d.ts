import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { TriggerRiskAnalysisRequest, TriggerRiskAnalysisSecurity } from "../models/operations/triggerriskanalysis.js";
import { MutationHookOptions } from "./_types.js";
export type RiskPoliciesTriggerMutationVariables = {
    request: TriggerRiskAnalysisRequest;
    security?: TriggerRiskAnalysisSecurity | undefined;
    options?: RequestOptions;
};
export type RiskPoliciesTriggerMutationData = void;
export type RiskPoliciesTriggerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * triggerRiskAnalysis risk
 *
 * @remarks
 * Manually trigger risk analysis for a policy, starting or signaling the drain workflow. Defaults to the most recent 100 unanalyzed messages; pass `limit=0` to backfill every unanalyzed message.
 */
export declare function useRiskPoliciesTriggerMutation(options?: MutationHookOptions<RiskPoliciesTriggerMutationData, RiskPoliciesTriggerMutationError, RiskPoliciesTriggerMutationVariables>): UseMutationResult<RiskPoliciesTriggerMutationData, RiskPoliciesTriggerMutationError, RiskPoliciesTriggerMutationVariables>;
export declare function mutationKeyRiskPoliciesTrigger(): MutationKey;
export declare function buildRiskPoliciesTriggerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskPoliciesTriggerMutationVariables) => Promise<RiskPoliciesTriggerMutationData>;
};
//# sourceMappingURL=riskPoliciesTrigger.d.ts.map