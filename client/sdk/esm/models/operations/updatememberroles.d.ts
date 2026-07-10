import * as z from "zod/v4-mini";
import { UpdateMemberRolesForm, UpdateMemberRolesForm$Outbound } from "../components/updatememberrolesform.js";
export type UpdateMemberRolesSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateMemberRolesRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    updateMemberRolesForm: UpdateMemberRolesForm;
};
/** @internal */
export type UpdateMemberRolesSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateMemberRolesSecurity$outboundSchema: z.ZodMiniType<UpdateMemberRolesSecurity$Outbound, UpdateMemberRolesSecurity>;
export declare function updateMemberRolesSecurityToJSON(updateMemberRolesSecurity: UpdateMemberRolesSecurity): string;
/** @internal */
export type UpdateMemberRolesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    UpdateMemberRolesForm: UpdateMemberRolesForm$Outbound;
};
/** @internal */
export declare const UpdateMemberRolesRequest$outboundSchema: z.ZodMiniType<UpdateMemberRolesRequest$Outbound, UpdateMemberRolesRequest>;
export declare function updateMemberRolesRequestToJSON(updateMemberRolesRequest: UpdateMemberRolesRequest): string;
//# sourceMappingURL=updatememberroles.d.ts.map