import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskBlock } from "../models/components/riskblock.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SubmitRiskBlockFeedbackRequest, SubmitRiskBlockFeedbackSecurity } from "../models/operations/submitriskblockfeedback.js";
import { MutationHookOptions } from "./_types.js";
export type RiskSubmitBlockFeedbackMutationVariables = {
    request: SubmitRiskBlockFeedbackRequest;
    security?: SubmitRiskBlockFeedbackSecurity | undefined;
    options?: RequestOptions;
};
export type RiskSubmitBlockFeedbackMutationData = RiskBlock;
export type RiskSubmitBlockFeedbackMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * submitRiskBlockFeedback risk
 *
 * @remarks
 * Record thumbs-up/thumbs-down feedback for a tool call block from the block page.
 */
export declare function useRiskSubmitBlockFeedbackMutation(options?: MutationHookOptions<RiskSubmitBlockFeedbackMutationData, RiskSubmitBlockFeedbackMutationError, RiskSubmitBlockFeedbackMutationVariables>): UseMutationResult<RiskSubmitBlockFeedbackMutationData, RiskSubmitBlockFeedbackMutationError, RiskSubmitBlockFeedbackMutationVariables>;
export declare function mutationKeyRiskSubmitBlockFeedback(): MutationKey;
export declare function buildRiskSubmitBlockFeedbackMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskSubmitBlockFeedbackMutationVariables) => Promise<RiskSubmitBlockFeedbackMutationData>;
};
//# sourceMappingURL=riskSubmitBlockFeedback.d.ts.map