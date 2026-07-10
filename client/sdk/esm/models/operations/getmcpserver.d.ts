import * as z from "zod/v4-mini";
export type GetMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetMcpServerSecurity = {
    option1?: GetMcpServerSecurityOption1 | undefined;
    option2?: GetMcpServerSecurityOption2 | undefined;
};
export type GetMcpServerRequest = {
    /**
     * The ID of the MCP server. Mutually exclusive with slug.
     */
    id?: string | undefined;
    /**
     * The slug of the MCP server. Mutually exclusive with id.
     */
    slug?: string | undefined;
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
export type GetMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<GetMcpServerSecurityOption1$Outbound, GetMcpServerSecurityOption1>;
export declare function getMcpServerSecurityOption1ToJSON(getMcpServerSecurityOption1: GetMcpServerSecurityOption1): string;
/** @internal */
export type GetMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<GetMcpServerSecurityOption2$Outbound, GetMcpServerSecurityOption2>;
export declare function getMcpServerSecurityOption2ToJSON(getMcpServerSecurityOption2: GetMcpServerSecurityOption2): string;
/** @internal */
export type GetMcpServerSecurity$Outbound = {
    Option1?: GetMcpServerSecurityOption1$Outbound | undefined;
    Option2?: GetMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetMcpServerSecurity$outboundSchema: z.ZodMiniType<GetMcpServerSecurity$Outbound, GetMcpServerSecurity>;
export declare function getMcpServerSecurityToJSON(getMcpServerSecurity: GetMcpServerSecurity): string;
/** @internal */
export type GetMcpServerRequest$Outbound = {
    id?: string | undefined;
    slug?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetMcpServerRequest$outboundSchema: z.ZodMiniType<GetMcpServerRequest$Outbound, GetMcpServerRequest>;
export declare function getMcpServerRequestToJSON(getMcpServerRequest: GetMcpServerRequest): string;
//# sourceMappingURL=getmcpserver.d.ts.map