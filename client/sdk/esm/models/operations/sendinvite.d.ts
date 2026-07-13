import * as z from "zod/v4-mini";
import {
  SendInviteRequestBody,
  SendInviteRequestBody$Outbound,
} from "../components/sendinviterequestbody.js";
export type SendInviteSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type SendInviteRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  sendInviteRequestBody: SendInviteRequestBody;
};
/** @internal */
export type SendInviteSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SendInviteSecurity$outboundSchema: z.ZodMiniType<
  SendInviteSecurity$Outbound,
  SendInviteSecurity
>;
export declare function sendInviteSecurityToJSON(
  sendInviteSecurity: SendInviteSecurity,
): string;
/** @internal */
export type SendInviteRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  SendInviteRequestBody: SendInviteRequestBody$Outbound;
};
/** @internal */
export declare const SendInviteRequest$outboundSchema: z.ZodMiniType<
  SendInviteRequest$Outbound,
  SendInviteRequest
>;
export declare function sendInviteRequestToJSON(
  sendInviteRequest: SendInviteRequest,
): string;
//# sourceMappingURL=sendinvite.d.ts.map
