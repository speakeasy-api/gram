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
  DeleteRiskEvalReviewRequest,
  DeleteRiskEvalReviewSecurity,
} from "../models/operations/deleteriskevalreview.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDeleteEvalReviewMutationVariables = {
  request: DeleteRiskEvalReviewRequest;
  security?: DeleteRiskEvalReviewSecurity | undefined;
  options?: RequestOptions;
};
export type RiskDeleteEvalReviewMutationData = void;
export type RiskDeleteEvalReviewMutationError =
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
 * deleteRiskEvalReview risk
 *
 * @remarks
 * Remove the current reviewer's verdict for one session (the toggle-off path). A reviewer can only clear their own verdict.
 */
export declare function useRiskDeleteEvalReviewMutation(
  options?: MutationHookOptions<
    RiskDeleteEvalReviewMutationData,
    RiskDeleteEvalReviewMutationError,
    RiskDeleteEvalReviewMutationVariables
  >,
): UseMutationResult<
  RiskDeleteEvalReviewMutationData,
  RiskDeleteEvalReviewMutationError,
  RiskDeleteEvalReviewMutationVariables
>;
export declare function mutationKeyRiskDeleteEvalReview(): MutationKey;
export declare function buildRiskDeleteEvalReviewMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: RiskDeleteEvalReviewMutationVariables,
  ) => Promise<RiskDeleteEvalReviewMutationData>;
};
//# sourceMappingURL=riskDeleteEvalReview.d.ts.map
