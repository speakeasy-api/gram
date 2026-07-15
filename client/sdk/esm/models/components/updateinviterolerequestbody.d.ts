import * as z from "zod/v4-mini";
export type UpdateInviteRoleRequestBody = {
  /**
   * WorkOS invitation ID.
   */
  invitationId: string;
  /**
   * Role ID to assign to the invitee.
   */
  roleId: string;
};
/** @internal */
export type UpdateInviteRoleRequestBody$Outbound = {
  invitation_id: string;
  role_id: string;
};
/** @internal */
export declare const UpdateInviteRoleRequestBody$outboundSchema: z.ZodMiniType<
  UpdateInviteRoleRequestBody$Outbound,
  UpdateInviteRoleRequestBody
>;
export declare function updateInviteRoleRequestBodyToJSON(
  updateInviteRoleRequestBody: UpdateInviteRoleRequestBody,
): string;
//# sourceMappingURL=updateinviterolerequestbody.d.ts.map
