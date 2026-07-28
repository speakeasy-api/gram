import * as z from "zod/v4-mini";
import {
  SubmitFeedbackRequestBody,
  SubmitFeedbackRequestBody$Outbound,
} from "../components/submitfeedbackrequestbody.js";
export type SubmitFeedbackSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SubmitFeedbackSecurityOption2 = {
  chatSessionsTokenHeaderGramChatSession: string;
};
export type SubmitFeedbackSecurity = {
  option1?: SubmitFeedbackSecurityOption1 | undefined;
  option2?: SubmitFeedbackSecurityOption2 | undefined;
};
export type SubmitFeedbackRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  /**
   * Chat Sessions token header
   */
  gramChatSession?: string | undefined;
  submitFeedbackRequestBody: SubmitFeedbackRequestBody;
};
/** @internal */
export type SubmitFeedbackSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SubmitFeedbackSecurityOption1$outboundSchema: z.ZodMiniType<
  SubmitFeedbackSecurityOption1$Outbound,
  SubmitFeedbackSecurityOption1
>;
export declare function submitFeedbackSecurityOption1ToJSON(
  submitFeedbackSecurityOption1: SubmitFeedbackSecurityOption1,
): string;
/** @internal */
export type SubmitFeedbackSecurityOption2$Outbound = {
  "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const SubmitFeedbackSecurityOption2$outboundSchema: z.ZodMiniType<
  SubmitFeedbackSecurityOption2$Outbound,
  SubmitFeedbackSecurityOption2
>;
export declare function submitFeedbackSecurityOption2ToJSON(
  submitFeedbackSecurityOption2: SubmitFeedbackSecurityOption2,
): string;
/** @internal */
export type SubmitFeedbackSecurity$Outbound = {
  Option1?: SubmitFeedbackSecurityOption1$Outbound | undefined;
  Option2?: SubmitFeedbackSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SubmitFeedbackSecurity$outboundSchema: z.ZodMiniType<
  SubmitFeedbackSecurity$Outbound,
  SubmitFeedbackSecurity
>;
export declare function submitFeedbackSecurityToJSON(
  submitFeedbackSecurity: SubmitFeedbackSecurity,
): string;
/** @internal */
export type SubmitFeedbackRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Chat-Session"?: string | undefined;
  SubmitFeedbackRequestBody: SubmitFeedbackRequestBody$Outbound;
};
/** @internal */
export declare const SubmitFeedbackRequest$outboundSchema: z.ZodMiniType<
  SubmitFeedbackRequest$Outbound,
  SubmitFeedbackRequest
>;
export declare function submitFeedbackRequestToJSON(
  submitFeedbackRequest: SubmitFeedbackRequest,
): string;
//# sourceMappingURL=submitfeedback.d.ts.map
