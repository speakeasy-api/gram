import * as z from "zod/v4-mini";
export type ListServerNameOverridesSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListServerNameOverridesSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListServerNameOverridesSecurity = {
    option1?: ListServerNameOverridesSecurityOption1 | undefined;
    option2?: ListServerNameOverridesSecurityOption2 | undefined;
};
export type ListServerNameOverridesRequest = {
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
export type ListServerNameOverridesSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListServerNameOverridesSecurityOption1$outboundSchema: z.ZodMiniType<ListServerNameOverridesSecurityOption1$Outbound, ListServerNameOverridesSecurityOption1>;
export declare function listServerNameOverridesSecurityOption1ToJSON(listServerNameOverridesSecurityOption1: ListServerNameOverridesSecurityOption1): string;
/** @internal */
export type ListServerNameOverridesSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListServerNameOverridesSecurityOption2$outboundSchema: z.ZodMiniType<ListServerNameOverridesSecurityOption2$Outbound, ListServerNameOverridesSecurityOption2>;
export declare function listServerNameOverridesSecurityOption2ToJSON(listServerNameOverridesSecurityOption2: ListServerNameOverridesSecurityOption2): string;
/** @internal */
export type ListServerNameOverridesSecurity$Outbound = {
    Option1?: ListServerNameOverridesSecurityOption1$Outbound | undefined;
    Option2?: ListServerNameOverridesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListServerNameOverridesSecurity$outboundSchema: z.ZodMiniType<ListServerNameOverridesSecurity$Outbound, ListServerNameOverridesSecurity>;
export declare function listServerNameOverridesSecurityToJSON(listServerNameOverridesSecurity: ListServerNameOverridesSecurity): string;
/** @internal */
export type ListServerNameOverridesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListServerNameOverridesRequest$outboundSchema: z.ZodMiniType<ListServerNameOverridesRequest$Outbound, ListServerNameOverridesRequest>;
export declare function listServerNameOverridesRequestToJSON(listServerNameOverridesRequest: ListServerNameOverridesRequest): string;
//# sourceMappingURL=listservernameoverrides.d.ts.map