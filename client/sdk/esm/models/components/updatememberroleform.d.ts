import * as z from "zod/v4-mini";
export type UpdateMemberRoleForm = {
    /**
     * The new role ID to assign.
     */
    roleId: string;
    /**
     * The user ID to update.
     */
    userId: string;
};
/** @internal */
export type UpdateMemberRoleForm$Outbound = {
    role_id: string;
    user_id: string;
};
/** @internal */
export declare const UpdateMemberRoleForm$outboundSchema: z.ZodMiniType<UpdateMemberRoleForm$Outbound, UpdateMemberRoleForm>;
export declare function updateMemberRoleFormToJSON(updateMemberRoleForm: UpdateMemberRoleForm): string;
//# sourceMappingURL=updatememberroleform.d.ts.map