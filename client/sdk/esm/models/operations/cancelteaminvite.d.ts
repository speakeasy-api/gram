import * as z from "zod/v4-mini";
export type CancelTeamInviteSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CancelTeamInviteRequest = {
  /**
   * The ID of the invite to cancel
   */
  inviteId: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type CancelTeamInviteSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CancelTeamInviteSecurity$outboundSchema: z.ZodMiniType<
  CancelTeamInviteSecurity$Outbound,
  CancelTeamInviteSecurity
>;
export declare function cancelTeamInviteSecurityToJSON(
  cancelTeamInviteSecurity: CancelTeamInviteSecurity,
): string;
/** @internal */
export type CancelTeamInviteRequest$Outbound = {
  invite_id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CancelTeamInviteRequest$outboundSchema: z.ZodMiniType<
  CancelTeamInviteRequest$Outbound,
  CancelTeamInviteRequest
>;
export declare function cancelTeamInviteRequestToJSON(
  cancelTeamInviteRequest: CancelTeamInviteRequest,
): string;
//# sourceMappingURL=cancelteaminvite.d.ts.map
