import * as z from "zod/v4-mini";
export type ListMCPCatalogSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListMCPCatalogSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListMCPCatalogSecurity = {
    option1?: ListMCPCatalogSecurityOption1 | undefined;
    option2?: ListMCPCatalogSecurityOption2 | undefined;
};
export type ListMCPCatalogRequest = {
    /**
     * Filter to a specific registry
     */
    registryId?: string | undefined;
    /**
     * Search query to filter servers by name
     */
    search?: string | undefined;
    /**
     * Pagination cursor
     */
    cursor?: string | undefined;
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
export type ListMCPCatalogSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListMCPCatalogSecurityOption1$outboundSchema: z.ZodMiniType<ListMCPCatalogSecurityOption1$Outbound, ListMCPCatalogSecurityOption1>;
export declare function listMCPCatalogSecurityOption1ToJSON(listMCPCatalogSecurityOption1: ListMCPCatalogSecurityOption1): string;
/** @internal */
export type ListMCPCatalogSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListMCPCatalogSecurityOption2$outboundSchema: z.ZodMiniType<ListMCPCatalogSecurityOption2$Outbound, ListMCPCatalogSecurityOption2>;
export declare function listMCPCatalogSecurityOption2ToJSON(listMCPCatalogSecurityOption2: ListMCPCatalogSecurityOption2): string;
/** @internal */
export type ListMCPCatalogSecurity$Outbound = {
    Option1?: ListMCPCatalogSecurityOption1$Outbound | undefined;
    Option2?: ListMCPCatalogSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListMCPCatalogSecurity$outboundSchema: z.ZodMiniType<ListMCPCatalogSecurity$Outbound, ListMCPCatalogSecurity>;
export declare function listMCPCatalogSecurityToJSON(listMCPCatalogSecurity: ListMCPCatalogSecurity): string;
/** @internal */
export type ListMCPCatalogRequest$Outbound = {
    registry_id?: string | undefined;
    search?: string | undefined;
    cursor?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListMCPCatalogRequest$outboundSchema: z.ZodMiniType<ListMCPCatalogRequest$Outbound, ListMCPCatalogRequest>;
export declare function listMCPCatalogRequestToJSON(listMCPCatalogRequest: ListMCPCatalogRequest): string;
//# sourceMappingURL=listmcpcatalog.d.ts.map