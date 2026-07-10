import * as z from "zod/v4-mini";
import { UpdateInviteRoleRequestBody, UpdateInviteRoleRequestBody$Outbound } from "../components/updateinviterolerequestbody.js";
export type UpdateInviteRoleSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateInviteRoleRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    updateInviteRoleRequestBody: UpdateInviteRoleRequestBody;
};
/** @internal */
export type UpdateInviteRoleSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateInviteRoleSecurity$outboundSchema: z.ZodMiniType<UpdateInviteRoleSecurity$Outbound, UpdateInviteRoleSecurity>;
export declare function updateInviteRoleSecurityToJSON(updateInviteRoleSecurity: UpdateInviteRoleSecurity): string;
/** @internal */
export type UpdateInviteRoleRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    UpdateInviteRoleRequestBody: UpdateInviteRoleRequestBody$Outbound;
};
/** @internal */
export declare const UpdateInviteRoleRequest$outboundSchema: z.ZodMiniType<UpdateInviteRoleRequest$Outbound, UpdateInviteRoleRequest>;
export declare function updateInviteRoleRequestToJSON(updateInviteRoleRequest: UpdateInviteRoleRequest): string;
//# sourceMappingURL=updateinviterole.d.ts.map