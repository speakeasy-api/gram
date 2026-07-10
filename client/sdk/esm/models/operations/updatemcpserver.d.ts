import * as z from "zod/v4-mini";
import { UpdateMcpServerForm, UpdateMcpServerForm$Outbound } from "../components/updatemcpserverform.js";
export type UpdateMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateMcpServerSecurity = {
    option1?: UpdateMcpServerSecurityOption1 | undefined;
    option2?: UpdateMcpServerSecurityOption2 | undefined;
};
export type UpdateMcpServerRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    updateMcpServerForm: UpdateMcpServerForm;
};
/** @internal */
export type UpdateMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<UpdateMcpServerSecurityOption1$Outbound, UpdateMcpServerSecurityOption1>;
export declare function updateMcpServerSecurityOption1ToJSON(updateMcpServerSecurityOption1: UpdateMcpServerSecurityOption1): string;
/** @internal */
export type UpdateMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<UpdateMcpServerSecurityOption2$Outbound, UpdateMcpServerSecurityOption2>;
export declare function updateMcpServerSecurityOption2ToJSON(updateMcpServerSecurityOption2: UpdateMcpServerSecurityOption2): string;
/** @internal */
export type UpdateMcpServerSecurity$Outbound = {
    Option1?: UpdateMcpServerSecurityOption1$Outbound | undefined;
    Option2?: UpdateMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateMcpServerSecurity$outboundSchema: z.ZodMiniType<UpdateMcpServerSecurity$Outbound, UpdateMcpServerSecurity>;
export declare function updateMcpServerSecurityToJSON(updateMcpServerSecurity: UpdateMcpServerSecurity): string;
/** @internal */
export type UpdateMcpServerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateMcpServerForm: UpdateMcpServerForm$Outbound;
};
/** @internal */
export declare const UpdateMcpServerRequest$outboundSchema: z.ZodMiniType<UpdateMcpServerRequest$Outbound, UpdateMcpServerRequest>;
export declare function updateMcpServerRequestToJSON(updateMcpServerRequest: UpdateMcpServerRequest): string;
//# sourceMappingURL=updatemcpserver.d.ts.map