import * as z from "zod/v4-mini";
export type GetToolsetSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetToolsetSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetToolsetSecurity = {
    option1?: GetToolsetSecurityOption1 | undefined;
    option2?: GetToolsetSecurityOption2 | undefined;
};
export type GetToolsetRequest = {
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
export type GetToolsetSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetToolsetSecurityOption1$outboundSchema: z.ZodMiniType<GetToolsetSecurityOption1$Outbound, GetToolsetSecurityOption1>;
export declare function getToolsetSecurityOption1ToJSON(getToolsetSecurityOption1: GetToolsetSecurityOption1): string;
/** @internal */
export type GetToolsetSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetToolsetSecurityOption2$outboundSchema: z.ZodMiniType<GetToolsetSecurityOption2$Outbound, GetToolsetSecurityOption2>;
export declare function getToolsetSecurityOption2ToJSON(getToolsetSecurityOption2: GetToolsetSecurityOption2): string;
/** @internal */
export type GetToolsetSecurity$Outbound = {
    Option1?: GetToolsetSecurityOption1$Outbound | undefined;
    Option2?: GetToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetToolsetSecurity$outboundSchema: z.ZodMiniType<GetToolsetSecurity$Outbound, GetToolsetSecurity>;
export declare function getToolsetSecurityToJSON(getToolsetSecurity: GetToolsetSecurity): string;
/** @internal */
export type GetToolsetRequest$Outbound = {
    slug: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetToolsetRequest$outboundSchema: z.ZodMiniType<GetToolsetRequest$Outbound, GetToolsetRequest>;
export declare function getToolsetRequestToJSON(getToolsetRequest: GetToolsetRequest): string;
//# sourceMappingURL=gettoolset.d.ts.map