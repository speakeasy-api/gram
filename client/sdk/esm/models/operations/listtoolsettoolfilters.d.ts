import * as z from "zod/v4-mini";
export type ListToolsetToolFiltersSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListToolsetToolFiltersSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListToolsetToolFiltersSecurity = {
    option1?: ListToolsetToolFiltersSecurityOption1 | undefined;
    option2?: ListToolsetToolFiltersSecurityOption2 | undefined;
};
export type ListToolsetToolFiltersRequest = {
    /**
     * The slug of the toolset
     */
    slug: string;
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
};
/** @internal */
export type ListToolsetToolFiltersSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolsetToolFiltersSecurityOption1$outboundSchema: z.ZodMiniType<ListToolsetToolFiltersSecurityOption1$Outbound, ListToolsetToolFiltersSecurityOption1>;
export declare function listToolsetToolFiltersSecurityOption1ToJSON(listToolsetToolFiltersSecurityOption1: ListToolsetToolFiltersSecurityOption1): string;
/** @internal */
export type ListToolsetToolFiltersSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolsetToolFiltersSecurityOption2$outboundSchema: z.ZodMiniType<ListToolsetToolFiltersSecurityOption2$Outbound, ListToolsetToolFiltersSecurityOption2>;
export declare function listToolsetToolFiltersSecurityOption2ToJSON(listToolsetToolFiltersSecurityOption2: ListToolsetToolFiltersSecurityOption2): string;
/** @internal */
export type ListToolsetToolFiltersSecurity$Outbound = {
    Option1?: ListToolsetToolFiltersSecurityOption1$Outbound | undefined;
    Option2?: ListToolsetToolFiltersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListToolsetToolFiltersSecurity$outboundSchema: z.ZodMiniType<ListToolsetToolFiltersSecurity$Outbound, ListToolsetToolFiltersSecurity>;
export declare function listToolsetToolFiltersSecurityToJSON(listToolsetToolFiltersSecurity: ListToolsetToolFiltersSecurity): string;
/** @internal */
export type ListToolsetToolFiltersRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolsetToolFiltersRequest$outboundSchema: z.ZodMiniType<ListToolsetToolFiltersRequest$Outbound, ListToolsetToolFiltersRequest>;
export declare function listToolsetToolFiltersRequestToJSON(listToolsetToolFiltersRequest: ListToolsetToolFiltersRequest): string;
//# sourceMappingURL=listtoolsettoolfilters.d.ts.map