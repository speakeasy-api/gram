import * as z from "zod/v4-mini";
export type GetTunneledMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetTunneledMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetTunneledMcpServerSecurity = {
    option1?: GetTunneledMcpServerSecurityOption1 | undefined;
    option2?: GetTunneledMcpServerSecurityOption2 | undefined;
};
export type GetTunneledMcpServerRequest = {
    /**
     * The ID of the tunneled MCP server
     */
    id: string;
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
export type GetTunneledMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetTunneledMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<GetTunneledMcpServerSecurityOption1$Outbound, GetTunneledMcpServerSecurityOption1>;
export declare function getTunneledMcpServerSecurityOption1ToJSON(getTunneledMcpServerSecurityOption1: GetTunneledMcpServerSecurityOption1): string;
/** @internal */
export type GetTunneledMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetTunneledMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<GetTunneledMcpServerSecurityOption2$Outbound, GetTunneledMcpServerSecurityOption2>;
export declare function getTunneledMcpServerSecurityOption2ToJSON(getTunneledMcpServerSecurityOption2: GetTunneledMcpServerSecurityOption2): string;
/** @internal */
export type GetTunneledMcpServerSecurity$Outbound = {
    Option1?: GetTunneledMcpServerSecurityOption1$Outbound | undefined;
    Option2?: GetTunneledMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetTunneledMcpServerSecurity$outboundSchema: z.ZodMiniType<GetTunneledMcpServerSecurity$Outbound, GetTunneledMcpServerSecurity>;
export declare function getTunneledMcpServerSecurityToJSON(getTunneledMcpServerSecurity: GetTunneledMcpServerSecurity): string;
/** @internal */
export type GetTunneledMcpServerRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetTunneledMcpServerRequest$outboundSchema: z.ZodMiniType<GetTunneledMcpServerRequest$Outbound, GetTunneledMcpServerRequest>;
export declare function getTunneledMcpServerRequestToJSON(getTunneledMcpServerRequest: GetTunneledMcpServerRequest): string;
//# sourceMappingURL=gettunneledmcpserver.d.ts.map