import * as z from "zod/v4-mini";
export type ListAllowedOriginsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListAllowedOriginsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListAllowedOriginsSecurity = {
    option1?: ListAllowedOriginsSecurityOption1 | undefined;
    option2?: ListAllowedOriginsSecurityOption2 | undefined;
};
export type ListAllowedOriginsRequest = {
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
export type ListAllowedOriginsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListAllowedOriginsSecurityOption1$outboundSchema: z.ZodMiniType<ListAllowedOriginsSecurityOption1$Outbound, ListAllowedOriginsSecurityOption1>;
export declare function listAllowedOriginsSecurityOption1ToJSON(listAllowedOriginsSecurityOption1: ListAllowedOriginsSecurityOption1): string;
/** @internal */
export type ListAllowedOriginsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListAllowedOriginsSecurityOption2$outboundSchema: z.ZodMiniType<ListAllowedOriginsSecurityOption2$Outbound, ListAllowedOriginsSecurityOption2>;
export declare function listAllowedOriginsSecurityOption2ToJSON(listAllowedOriginsSecurityOption2: ListAllowedOriginsSecurityOption2): string;
/** @internal */
export type ListAllowedOriginsSecurity$Outbound = {
    Option1?: ListAllowedOriginsSecurityOption1$Outbound | undefined;
    Option2?: ListAllowedOriginsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListAllowedOriginsSecurity$outboundSchema: z.ZodMiniType<ListAllowedOriginsSecurity$Outbound, ListAllowedOriginsSecurity>;
export declare function listAllowedOriginsSecurityToJSON(listAllowedOriginsSecurity: ListAllowedOriginsSecurity): string;
/** @internal */
export type ListAllowedOriginsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListAllowedOriginsRequest$outboundSchema: z.ZodMiniType<ListAllowedOriginsRequest$Outbound, ListAllowedOriginsRequest>;
export declare function listAllowedOriginsRequestToJSON(listAllowedOriginsRequest: ListAllowedOriginsRequest): string;
//# sourceMappingURL=listallowedorigins.d.ts.map