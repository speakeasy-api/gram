import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type UpdateMemberRoleSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateMemberRoleRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    updateMemberRoleForm: components.UpdateMemberRoleForm;
};
/** @internal */
export type UpdateMemberRoleSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateMemberRoleSecurity$outboundSchema: z.ZodMiniType<UpdateMemberRoleSecurity$Outbound, UpdateMemberRoleSecurity>;
export declare function updateMemberRoleSecurityToJSON(updateMemberRoleSecurity: UpdateMemberRoleSecurity): string;
/** @internal */
export type UpdateMemberRoleRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    UpdateMemberRoleForm: components.UpdateMemberRoleForm$Outbound;
};
/** @internal */
export declare const UpdateMemberRoleRequest$outboundSchema: z.ZodMiniType<UpdateMemberRoleRequest$Outbound, UpdateMemberRoleRequest>;
export declare function updateMemberRoleRequestToJSON(updateMemberRoleRequest: UpdateMemberRoleRequest): string;
//# sourceMappingURL=updatememberrole.d.ts.map