import * as z from "zod/v4-mini";
export type ListMCPRegistriesSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListMCPRegistriesSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListMCPRegistriesSecurity = {
    option1?: ListMCPRegistriesSecurityOption1 | undefined;
    option2?: ListMCPRegistriesSecurityOption2 | undefined;
};
export type ListMCPRegistriesRequest = {
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
export type ListMCPRegistriesSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListMCPRegistriesSecurityOption1$outboundSchema: z.ZodMiniType<ListMCPRegistriesSecurityOption1$Outbound, ListMCPRegistriesSecurityOption1>;
export declare function listMCPRegistriesSecurityOption1ToJSON(listMCPRegistriesSecurityOption1: ListMCPRegistriesSecurityOption1): string;
/** @internal */
export type ListMCPRegistriesSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListMCPRegistriesSecurityOption2$outboundSchema: z.ZodMiniType<ListMCPRegistriesSecurityOption2$Outbound, ListMCPRegistriesSecurityOption2>;
export declare function listMCPRegistriesSecurityOption2ToJSON(listMCPRegistriesSecurityOption2: ListMCPRegistriesSecurityOption2): string;
/** @internal */
export type ListMCPRegistriesSecurity$Outbound = {
    Option1?: ListMCPRegistriesSecurityOption1$Outbound | undefined;
    Option2?: ListMCPRegistriesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListMCPRegistriesSecurity$outboundSchema: z.ZodMiniType<ListMCPRegistriesSecurity$Outbound, ListMCPRegistriesSecurity>;
export declare function listMCPRegistriesSecurityToJSON(listMCPRegistriesSecurity: ListMCPRegistriesSecurity): string;
/** @internal */
export type ListMCPRegistriesRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListMCPRegistriesRequest$outboundSchema: z.ZodMiniType<ListMCPRegistriesRequest$Outbound, ListMCPRegistriesRequest>;
export declare function listMCPRegistriesRequestToJSON(listMCPRegistriesRequest: ListMCPRegistriesRequest): string;
//# sourceMappingURL=listmcpregistries.d.ts.map