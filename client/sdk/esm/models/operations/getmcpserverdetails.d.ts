import * as z from "zod/v4-mini";
export type GetMCPServerDetailsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetMCPServerDetailsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetMCPServerDetailsSecurity = {
    option1?: GetMCPServerDetailsSecurityOption1 | undefined;
    option2?: GetMCPServerDetailsSecurityOption2 | undefined;
};
export type GetMCPServerDetailsRequest = {
    /**
     * ID of the registry
     */
    registryId: string;
    /**
     * Server specifier (e.g., 'io.github.user/server')
     */
    serverSpecifier: string;
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
export type GetMCPServerDetailsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetMCPServerDetailsSecurityOption1$outboundSchema: z.ZodMiniType<GetMCPServerDetailsSecurityOption1$Outbound, GetMCPServerDetailsSecurityOption1>;
export declare function getMCPServerDetailsSecurityOption1ToJSON(getMCPServerDetailsSecurityOption1: GetMCPServerDetailsSecurityOption1): string;
/** @internal */
export type GetMCPServerDetailsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetMCPServerDetailsSecurityOption2$outboundSchema: z.ZodMiniType<GetMCPServerDetailsSecurityOption2$Outbound, GetMCPServerDetailsSecurityOption2>;
export declare function getMCPServerDetailsSecurityOption2ToJSON(getMCPServerDetailsSecurityOption2: GetMCPServerDetailsSecurityOption2): string;
/** @internal */
export type GetMCPServerDetailsSecurity$Outbound = {
    Option1?: GetMCPServerDetailsSecurityOption1$Outbound | undefined;
    Option2?: GetMCPServerDetailsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetMCPServerDetailsSecurity$outboundSchema: z.ZodMiniType<GetMCPServerDetailsSecurity$Outbound, GetMCPServerDetailsSecurity>;
export declare function getMCPServerDetailsSecurityToJSON(getMCPServerDetailsSecurity: GetMCPServerDetailsSecurity): string;
/** @internal */
export type GetMCPServerDetailsRequest$Outbound = {
    registry_id: string;
    server_specifier: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetMCPServerDetailsRequest$outboundSchema: z.ZodMiniType<GetMCPServerDetailsRequest$Outbound, GetMCPServerDetailsRequest>;
export declare function getMCPServerDetailsRequestToJSON(getMCPServerDetailsRequest: GetMCPServerDetailsRequest): string;
//# sourceMappingURL=getmcpserverdetails.d.ts.map