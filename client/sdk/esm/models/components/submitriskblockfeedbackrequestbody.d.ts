import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Feedback sentiment.
 */
export declare const Sentiment: {
  readonly Up: "up";
  readonly Down: "down";
};
/**
 * Feedback sentiment.
 */
export type Sentiment = ClosedEnum<typeof Sentiment>;
export type SubmitRiskBlockFeedbackRequestBody = {
  /**
   * The block ID (the underlying risk result ID).
   */
  id: string;
  /**
   * Feedback sentiment.
   */
  sentiment: Sentiment;
};
/** @internal */
export declare const Sentiment$outboundSchema: z.ZodMiniEnum<typeof Sentiment>;
/** @internal */
export type SubmitRiskBlockFeedbackRequestBody$Outbound = {
  id: string;
  sentiment: string;
};
/** @internal */
export declare const SubmitRiskBlockFeedbackRequestBody$outboundSchema: z.ZodMiniType<
  SubmitRiskBlockFeedbackRequestBody$Outbound,
  SubmitRiskBlockFeedbackRequestBody
>;
export declare function submitRiskBlockFeedbackRequestBodyToJSON(
  submitRiskBlockFeedbackRequestBody: SubmitRiskBlockFeedbackRequestBody,
): string;
//# sourceMappingURL=submitriskblockfeedbackrequestbody.d.ts.map
