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
export type RiskTriggerAnalysisMutationVariables = {
    request: operations.TriggerRiskAnalysisRequest;
    security?: operations.TriggerRiskAnalysisSecurity | undefined;
    options?: RequestOptions;
};
export type RiskTriggerAnalysisMutationData = void;
export type RiskTriggerAnalysisMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * triggerRiskAnalysis risk
 *
 * @remarks
 * Manually trigger risk analysis for a policy, starting or signaling the drain workflow.
 */
export declare function useRiskTriggerAnalysisMutation(options?: MutationHookOptions<RiskTriggerAnalysisMutationData, RiskTriggerAnalysisMutationError, RiskTriggerAnalysisMutationVariables>): UseMutationResult<RiskTriggerAnalysisMutationData, RiskTriggerAnalysisMutationError, RiskTriggerAnalysisMutationVariables>;
export declare function mutationKeyRiskTriggerAnalysis(): MutationKey;
export declare function buildRiskTriggerAnalysisMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskTriggerAnalysisMutationVariables) => Promise<RiskTriggerAnalysisMutationData>;
};
//# sourceMappingURL=riskTriggerAnalysis.d.ts.map