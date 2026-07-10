import * as z from "zod/v4-mini";
export type ListTemplatesSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListTemplatesSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTemplatesSecurity = {
    option1?: ListTemplatesSecurityOption1 | undefined;
    option2?: ListTemplatesSecurityOption2 | undefined;
};
export type ListTemplatesRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
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
export type ListTemplatesSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListTemplatesSecurityOption1$outboundSchema: z.ZodMiniType<ListTemplatesSecurityOption1$Outbound, ListTemplatesSecurityOption1>;
export declare function listTemplatesSecurityOption1ToJSON(listTemplatesSecurityOption1: ListTemplatesSecurityOption1): string;
/** @internal */
export type ListTemplatesSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTemplatesSecurityOption2$outboundSchema: z.ZodMiniType<ListTemplatesSecurityOption2$Outbound, ListTemplatesSecurityOption2>;
export declare function listTemplatesSecurityOption2ToJSON(listTemplatesSecurityOption2: ListTemplatesSecurityOption2): string;
/** @internal */
export type ListTemplatesSecurity$Outbound = {
    Option1?: ListTemplatesSecurityOption1$Outbound | undefined;
    Option2?: ListTemplatesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListTemplatesSecurity$outboundSchema: z.ZodMiniType<ListTemplatesSecurity$Outbound, ListTemplatesSecurity>;
export declare function listTemplatesSecurityToJSON(listTemplatesSecurity: ListTemplatesSecurity): string;
/** @internal */
export type ListTemplatesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTemplatesRequest$outboundSchema: z.ZodMiniType<ListTemplatesRequest$Outbound, ListTemplatesRequest>;
export declare function listTemplatesRequestToJSON(listTemplatesRequest: ListTemplatesRequest): string;
//# sourceMappingURL=listtemplates.d.ts.map