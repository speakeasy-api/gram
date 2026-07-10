import * as z from "zod/v4-mini";
import { CreateRoleForm, CreateRoleForm$Outbound } from "../components/createroleform.js";
export type CreateRoleSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type CreateRoleRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    createRoleForm: CreateRoleForm;
};
/** @internal */
export type CreateRoleSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateRoleSecurity$outboundSchema: z.ZodMiniType<CreateRoleSecurity$Outbound, CreateRoleSecurity>;
export declare function createRoleSecurityToJSON(createRoleSecurity: CreateRoleSecurity): string;
/** @internal */
export type CreateRoleRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    CreateRoleForm: CreateRoleForm$Outbound;
};
/** @internal */
export declare const CreateRoleRequest$outboundSchema: z.ZodMiniType<CreateRoleRequest$Outbound, CreateRoleRequest>;
export declare function createRoleRequestToJSON(createRoleRequest: CreateRoleRequest): string;
//# sourceMappingURL=createrole.d.ts.map