import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type InviteTeamMemberSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type InviteTeamMemberRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  inviteMemberForm: components.InviteMemberForm;
};
/** @internal */
export type InviteTeamMemberSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const InviteTeamMemberSecurity$outboundSchema: z.ZodMiniType<
  InviteTeamMemberSecurity$Outbound,
  InviteTeamMemberSecurity
>;
export declare function inviteTeamMemberSecurityToJSON(
  inviteTeamMemberSecurity: InviteTeamMemberSecurity,
): string;
/** @internal */
export type InviteTeamMemberRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  InviteMemberForm: components.InviteMemberForm$Outbound;
};
/** @internal */
export declare const InviteTeamMemberRequest$outboundSchema: z.ZodMiniType<
  InviteTeamMemberRequest$Outbound,
  InviteTeamMemberRequest
>;
export declare function inviteTeamMemberRequestToJSON(
  inviteTeamMemberRequest: InviteTeamMemberRequest,
): string;
//# sourceMappingURL=inviteteammember.d.ts.map
