import * as z from "zod/v4-mini";
export type UpdateMemberRolesForm = {
  /**
   * The role IDs to assign. Replaces all existing role assignments.
   */
  roleIds: Array<string>;
  /**
   * The user ID to update.
   */
  userId: string;
};
/** @internal */
export type UpdateMemberRolesForm$Outbound = {
  role_ids: Array<string>;
  user_id: string;
};
/** @internal */
export declare const UpdateMemberRolesForm$outboundSchema: z.ZodMiniType<
  UpdateMemberRolesForm$Outbound,
  UpdateMemberRolesForm
>;
export declare function updateMemberRolesFormToJSON(
  updateMemberRolesForm: UpdateMemberRolesForm,
): string;
//# sourceMappingURL=updatememberrolesform.d.ts.map
