import * as z from "zod/v4-mini";
export type ListUserSessionFacetsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListUserSessionFacetsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListUserSessionFacetsSecurity = {
    option1?: ListUserSessionFacetsSecurityOption1 | undefined;
    option2?: ListUserSessionFacetsSecurityOption2 | undefined;
};
export type ListUserSessionFacetsRequest = {
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
export type ListUserSessionFacetsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListUserSessionFacetsSecurityOption1$outboundSchema: z.ZodMiniType<ListUserSessionFacetsSecurityOption1$Outbound, ListUserSessionFacetsSecurityOption1>;
export declare function listUserSessionFacetsSecurityOption1ToJSON(listUserSessionFacetsSecurityOption1: ListUserSessionFacetsSecurityOption1): string;
/** @internal */
export type ListUserSessionFacetsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListUserSessionFacetsSecurityOption2$outboundSchema: z.ZodMiniType<ListUserSessionFacetsSecurityOption2$Outbound, ListUserSessionFacetsSecurityOption2>;
export declare function listUserSessionFacetsSecurityOption2ToJSON(listUserSessionFacetsSecurityOption2: ListUserSessionFacetsSecurityOption2): string;
/** @internal */
export type ListUserSessionFacetsSecurity$Outbound = {
    Option1?: ListUserSessionFacetsSecurityOption1$Outbound | undefined;
    Option2?: ListUserSessionFacetsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListUserSessionFacetsSecurity$outboundSchema: z.ZodMiniType<ListUserSessionFacetsSecurity$Outbound, ListUserSessionFacetsSecurity>;
export declare function listUserSessionFacetsSecurityToJSON(listUserSessionFacetsSecurity: ListUserSessionFacetsSecurity): string;
/** @internal */
export type ListUserSessionFacetsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListUserSessionFacetsRequest$outboundSchema: z.ZodMiniType<ListUserSessionFacetsRequest$Outbound, ListUserSessionFacetsRequest>;
export declare function listUserSessionFacetsRequestToJSON(listUserSessionFacetsRequest: ListUserSessionFacetsRequest): string;
//# sourceMappingURL=listusersessionfacets.d.ts.map