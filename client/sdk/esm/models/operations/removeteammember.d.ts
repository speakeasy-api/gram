import * as z from "zod/v4-mini";
export type RemoveTeamMemberSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type RemoveTeamMemberRequest = {
  /**
   * The ID of the organization
   */
  organizationId: string;
  /**
   * The ID of the user to remove
   */
  userId: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type RemoveTeamMemberSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemoveTeamMemberSecurity$outboundSchema: z.ZodMiniType<
  RemoveTeamMemberSecurity$Outbound,
  RemoveTeamMemberSecurity
>;
export declare function removeTeamMemberSecurityToJSON(
  removeTeamMemberSecurity: RemoveTeamMemberSecurity,
): string;
/** @internal */
export type RemoveTeamMemberRequest$Outbound = {
  organization_id: string;
  user_id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemoveTeamMemberRequest$outboundSchema: z.ZodMiniType<
  RemoveTeamMemberRequest$Outbound,
  RemoveTeamMemberRequest
>;
export declare function removeTeamMemberRequestToJSON(
  removeTeamMemberRequest: RemoveTeamMemberRequest,
): string;
//# sourceMappingURL=removeteammember.d.ts.map
