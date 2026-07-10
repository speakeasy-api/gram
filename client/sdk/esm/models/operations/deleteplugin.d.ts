import * as z from "zod/v4-mini";
export type DeletePluginSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeletePluginRequest = {
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type DeletePluginSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeletePluginSecurity$outboundSchema: z.ZodMiniType<DeletePluginSecurity$Outbound, DeletePluginSecurity>;
export declare function deletePluginSecurityToJSON(deletePluginSecurity: DeletePluginSecurity): string;
/** @internal */
export type DeletePluginRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeletePluginRequest$outboundSchema: z.ZodMiniType<DeletePluginRequest$Outbound, DeletePluginRequest>;
export declare function deletePluginRequestToJSON(deletePluginRequest: DeletePluginRequest): string;
//# sourceMappingURL=deleteplugin.d.ts.map