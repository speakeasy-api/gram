import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The reviewer's ground-truth verdict.
 */
export declare const Verdict: {
  readonly Correct: "correct";
  readonly FalsePositive: "false_positive";
  readonly Missed: "missed";
};
/**
 * The reviewer's ground-truth verdict.
 */
export type Verdict = ClosedEnum<typeof Verdict>;
export type RiskPolicyEvalReview = {
  /**
   * The chat session being judged.
   */
  chatId: string;
  /**
   * When the verdict was first recorded.
   */
  createdAt: Date;
  /**
   * The review ID.
   */
  id: string;
  /**
   * The prompt-based policy the verdict belongs to.
   */
  policyId: string;
  /**
   * The policy version in effect when the verdict was recorded (provenance).
   */
  policyVersion: number;
  /**
   * User id of the reviewer who recorded the verdict.
   */
  reviewedBy: string;
  /**
   * When the verdict was last updated.
   */
  updatedAt: Date;
  /**
   * The reviewer's ground-truth verdict.
   */
  verdict: Verdict;
};
/** @internal */
export declare const Verdict$inboundSchema: z.ZodMiniEnum<typeof Verdict>;
/** @internal */
export declare const RiskPolicyEvalReview$inboundSchema: z.ZodMiniType<
  RiskPolicyEvalReview,
  unknown
>;
export declare function riskPolicyEvalReviewFromJSON(
  jsonString: string,
): SafeParseResult<RiskPolicyEvalReview, SDKValidationError>;
//# sourceMappingURL=riskpolicyevalreview.d.ts.map
