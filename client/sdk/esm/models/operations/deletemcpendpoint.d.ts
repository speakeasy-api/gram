import * as z from "zod/v4-mini";
export type DeleteMcpEndpointSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteMcpEndpointSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteMcpEndpointSecurity = {
    option1?: DeleteMcpEndpointSecurityOption1 | undefined;
    option2?: DeleteMcpEndpointSecurityOption2 | undefined;
};
export type DeleteMcpEndpointRequest = {
    /**
     * The ID of the MCP endpoint to delete
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
export type DeleteMcpEndpointSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteMcpEndpointSecurityOption1$outboundSchema: z.ZodMiniType<DeleteMcpEndpointSecurityOption1$Outbound, DeleteMcpEndpointSecurityOption1>;
export declare function deleteMcpEndpointSecurityOption1ToJSON(deleteMcpEndpointSecurityOption1: DeleteMcpEndpointSecurityOption1): string;
/** @internal */
export type DeleteMcpEndpointSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteMcpEndpointSecurityOption2$outboundSchema: z.ZodMiniType<DeleteMcpEndpointSecurityOption2$Outbound, DeleteMcpEndpointSecurityOption2>;
export declare function deleteMcpEndpointSecurityOption2ToJSON(deleteMcpEndpointSecurityOption2: DeleteMcpEndpointSecurityOption2): string;
/** @internal */
export type DeleteMcpEndpointSecurity$Outbound = {
    Option1?: DeleteMcpEndpointSecurityOption1$Outbound | undefined;
    Option2?: DeleteMcpEndpointSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteMcpEndpointSecurity$outboundSchema: z.ZodMiniType<DeleteMcpEndpointSecurity$Outbound, DeleteMcpEndpointSecurity>;
export declare function deleteMcpEndpointSecurityToJSON(deleteMcpEndpointSecurity: DeleteMcpEndpointSecurity): string;
/** @internal */
export type DeleteMcpEndpointRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteMcpEndpointRequest$outboundSchema: z.ZodMiniType<DeleteMcpEndpointRequest$Outbound, DeleteMcpEndpointRequest>;
export declare function deleteMcpEndpointRequestToJSON(deleteMcpEndpointRequest: DeleteMcpEndpointRequest): string;
//# sourceMappingURL=deletemcpendpoint.d.ts.map