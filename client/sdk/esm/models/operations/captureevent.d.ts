import * as z from "zod/v4-mini";
import {
  CaptureEventPayload,
  CaptureEventPayload$Outbound,
} from "../components/captureeventpayload.js";
export type CaptureEventSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CaptureEventSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CaptureEventSecurityOption3 = {
  chatSessionsTokenHeaderGramChatSession: string;
};
export type CaptureEventSecurity = {
  option1?: CaptureEventSecurityOption1 | undefined;
  option2?: CaptureEventSecurityOption2 | undefined;
  option3?: CaptureEventSecurityOption3 | undefined;
};
export type CaptureEventRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
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
  captureEventPayload: CaptureEventPayload;
};
/** @internal */
export type CaptureEventSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CaptureEventSecurityOption1$outboundSchema: z.ZodMiniType<
  CaptureEventSecurityOption1$Outbound,
  CaptureEventSecurityOption1
>;
export declare function captureEventSecurityOption1ToJSON(
  captureEventSecurityOption1: CaptureEventSecurityOption1,
): string;
/** @internal */
export type CaptureEventSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CaptureEventSecurityOption2$outboundSchema: z.ZodMiniType<
  CaptureEventSecurityOption2$Outbound,
  CaptureEventSecurityOption2
>;
export declare function captureEventSecurityOption2ToJSON(
  captureEventSecurityOption2: CaptureEventSecurityOption2,
): string;
/** @internal */
export type CaptureEventSecurityOption3$Outbound = {
  "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const CaptureEventSecurityOption3$outboundSchema: z.ZodMiniType<
  CaptureEventSecurityOption3$Outbound,
  CaptureEventSecurityOption3
>;
export declare function captureEventSecurityOption3ToJSON(
  captureEventSecurityOption3: CaptureEventSecurityOption3,
): string;
/** @internal */
export type CaptureEventSecurity$Outbound = {
  Option1?: CaptureEventSecurityOption1$Outbound | undefined;
  Option2?: CaptureEventSecurityOption2$Outbound | undefined;
  Option3?: CaptureEventSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const CaptureEventSecurity$outboundSchema: z.ZodMiniType<
  CaptureEventSecurity$Outbound,
  CaptureEventSecurity
>;
export declare function captureEventSecurityToJSON(
  captureEventSecurity: CaptureEventSecurity,
): string;
/** @internal */
export type CaptureEventRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Chat-Session"?: string | undefined;
  CaptureEventPayload: CaptureEventPayload$Outbound;
};
/** @internal */
export declare const CaptureEventRequest$outboundSchema: z.ZodMiniType<
  CaptureEventRequest$Outbound,
  CaptureEventRequest
>;
export declare function captureEventRequestToJSON(
  captureEventRequest: CaptureEventRequest,
): string;
//# sourceMappingURL=captureevent.d.ts.map
