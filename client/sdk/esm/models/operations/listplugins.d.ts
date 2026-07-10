import * as z from "zod/v4-mini";
export type ListPluginsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListPluginsRequest = {
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
export type ListPluginsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListPluginsSecurity$outboundSchema: z.ZodMiniType<ListPluginsSecurity$Outbound, ListPluginsSecurity>;
export declare function listPluginsSecurityToJSON(listPluginsSecurity: ListPluginsSecurity): string;
/** @internal */
export type ListPluginsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListPluginsRequest$outboundSchema: z.ZodMiniType<ListPluginsRequest$Outbound, ListPluginsRequest>;
export declare function listPluginsRequestToJSON(listPluginsRequest: ListPluginsRequest): string;
//# sourceMappingURL=listplugins.d.ts.map