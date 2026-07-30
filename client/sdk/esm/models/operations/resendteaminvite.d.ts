import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type ResendTeamInviteSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ResendTeamInviteRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  resendInviteRequestBody: components.ResendInviteRequestBody;
};
/** @internal */
export type ResendTeamInviteSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ResendTeamInviteSecurity$outboundSchema: z.ZodMiniType<
  ResendTeamInviteSecurity$Outbound,
  ResendTeamInviteSecurity
>;
export declare function resendTeamInviteSecurityToJSON(
  resendTeamInviteSecurity: ResendTeamInviteSecurity,
): string;
/** @internal */
export type ResendTeamInviteRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  ResendInviteRequestBody: components.ResendInviteRequestBody$Outbound;
};
/** @internal */
export declare const ResendTeamInviteRequest$outboundSchema: z.ZodMiniType<
  ResendTeamInviteRequest$Outbound,
  ResendTeamInviteRequest
>;
export declare function resendTeamInviteRequestToJSON(
  resendTeamInviteRequest: ResendTeamInviteRequest,
): string;
//# sourceMappingURL=resendteaminvite.d.ts.map
