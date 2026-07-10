import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskPolicyEvalReview } from "./riskpolicyevalreview.js";
export type ListRiskEvalReviewsResult = {
    /**
     * The active review set for the policy.
     */
    reviews: Array<RiskPolicyEvalReview>;
};
/** @internal */
export declare const ListRiskEvalReviewsResult$inboundSchema: z.ZodMiniType<ListRiskEvalReviewsResult, unknown>;
export declare function listRiskEvalReviewsResultFromJSON(jsonString: string): SafeParseResult<ListRiskEvalReviewsResult, SDKValidationError>;
//# sourceMappingURL=listriskevalreviewsresult.d.ts.map