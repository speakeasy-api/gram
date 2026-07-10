import * as z from "zod/v4-mini";
export type GetMcpEndpointSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetMcpEndpointSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetMcpEndpointSecurity = {
    option1?: GetMcpEndpointSecurityOption1 | undefined;
    option2?: GetMcpEndpointSecurityOption2 | undefined;
};
export type GetMcpEndpointRequest = {
    /**
     * The ID of the MCP endpoint
     */
    id?: string | undefined;
    /**
     * The ID of the custom domain the endpoint slug is registered under. Omit to look up a platform-domain endpoint.
     */
    customDomainId?: string | undefined;
    /**
     * The slug to look up
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
export type GetMcpEndpointSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetMcpEndpointSecurityOption1$outboundSchema: z.ZodMiniType<GetMcpEndpointSecurityOption1$Outbound, GetMcpEndpointSecurityOption1>;
export declare function getMcpEndpointSecurityOption1ToJSON(getMcpEndpointSecurityOption1: GetMcpEndpointSecurityOption1): string;
/** @internal */
export type GetMcpEndpointSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetMcpEndpointSecurityOption2$outboundSchema: z.ZodMiniType<GetMcpEndpointSecurityOption2$Outbound, GetMcpEndpointSecurityOption2>;
export declare function getMcpEndpointSecurityOption2ToJSON(getMcpEndpointSecurityOption2: GetMcpEndpointSecurityOption2): string;
/** @internal */
export type GetMcpEndpointSecurity$Outbound = {
    Option1?: GetMcpEndpointSecurityOption1$Outbound | undefined;
    Option2?: GetMcpEndpointSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetMcpEndpointSecurity$outboundSchema: z.ZodMiniType<GetMcpEndpointSecurity$Outbound, GetMcpEndpointSecurity>;
export declare function getMcpEndpointSecurityToJSON(getMcpEndpointSecurity: GetMcpEndpointSecurity): string;
/** @internal */
export type GetMcpEndpointRequest$Outbound = {
    id?: string | undefined;
    custom_domain_id?: string | undefined;
    slug?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetMcpEndpointRequest$outboundSchema: z.ZodMiniType<GetMcpEndpointRequest$Outbound, GetMcpEndpointRequest>;
export declare function getMcpEndpointRequestToJSON(getMcpEndpointRequest: GetMcpEndpointRequest): string;
//# sourceMappingURL=getmcpendpoint.d.ts.map