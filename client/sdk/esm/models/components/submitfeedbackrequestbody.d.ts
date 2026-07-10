import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * User feedback: success or failure
 */
export declare const Feedback: {
  readonly Success: "success";
  readonly Failure: "failure";
};
/**
 * User feedback: success or failure
 */
export type Feedback = ClosedEnum<typeof Feedback>;
export type SubmitFeedbackRequestBody = {
  /**
   * User feedback: success or failure
   */
  feedback: Feedback;
  /**
   * The ID of the chat
   */
  id: string;
};
/** @internal */
export declare const Feedback$outboundSchema: z.ZodMiniEnum<typeof Feedback>;
/** @internal */
export type SubmitFeedbackRequestBody$Outbound = {
  feedback: string;
  id: string;
};
/** @internal */
export declare const SubmitFeedbackRequestBody$outboundSchema: z.ZodMiniType<
  SubmitFeedbackRequestBody$Outbound,
  SubmitFeedbackRequestBody
>;
export declare function submitFeedbackRequestBodyToJSON(
  submitFeedbackRequestBody: SubmitFeedbackRequestBody,
): string;
//# sourceMappingURL=submitfeedbackrequestbody.d.ts.map
