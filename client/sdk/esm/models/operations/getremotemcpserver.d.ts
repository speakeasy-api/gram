import * as z from "zod/v4-mini";
export type GetRemoteMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRemoteMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRemoteMcpServerSecurity = {
    option1?: GetRemoteMcpServerSecurityOption1 | undefined;
    option2?: GetRemoteMcpServerSecurityOption2 | undefined;
};
export type GetRemoteMcpServerRequest = {
    /**
     * The ID of the remote MCP server. Mutually exclusive with slug.
     */
    id?: string | undefined;
    /**
     * The slug of the remote MCP server. Mutually exclusive with id.
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
export type GetRemoteMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRemoteMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<GetRemoteMcpServerSecurityOption1$Outbound, GetRemoteMcpServerSecurityOption1>;
export declare function getRemoteMcpServerSecurityOption1ToJSON(getRemoteMcpServerSecurityOption1: GetRemoteMcpServerSecurityOption1): string;
/** @internal */
export type GetRemoteMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRemoteMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<GetRemoteMcpServerSecurityOption2$Outbound, GetRemoteMcpServerSecurityOption2>;
export declare function getRemoteMcpServerSecurityOption2ToJSON(getRemoteMcpServerSecurityOption2: GetRemoteMcpServerSecurityOption2): string;
/** @internal */
export type GetRemoteMcpServerSecurity$Outbound = {
    Option1?: GetRemoteMcpServerSecurityOption1$Outbound | undefined;
    Option2?: GetRemoteMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRemoteMcpServerSecurity$outboundSchema: z.ZodMiniType<GetRemoteMcpServerSecurity$Outbound, GetRemoteMcpServerSecurity>;
export declare function getRemoteMcpServerSecurityToJSON(getRemoteMcpServerSecurity: GetRemoteMcpServerSecurity): string;
/** @internal */
export type GetRemoteMcpServerRequest$Outbound = {
    id?: string | undefined;
    slug?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRemoteMcpServerRequest$outboundSchema: z.ZodMiniType<GetRemoteMcpServerRequest$Outbound, GetRemoteMcpServerRequest>;
export declare function getRemoteMcpServerRequestToJSON(getRemoteMcpServerRequest: GetRemoteMcpServerRequest): string;
//# sourceMappingURL=getremotemcpserver.d.ts.map