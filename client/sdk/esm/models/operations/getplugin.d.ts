import * as z from "zod/v4-mini";
export type GetPluginSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetPluginRequest = {
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
export type GetPluginSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetPluginSecurity$outboundSchema: z.ZodMiniType<GetPluginSecurity$Outbound, GetPluginSecurity>;
export declare function getPluginSecurityToJSON(getPluginSecurity: GetPluginSecurity): string;
/** @internal */
export type GetPluginRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetPluginRequest$outboundSchema: z.ZodMiniType<GetPluginRequest$Outbound, GetPluginRequest>;
export declare function getPluginRequestToJSON(getPluginRequest: GetPluginRequest): string;
//# sourceMappingURL=getplugin.d.ts.map