import * as z from "zod/v4-mini";
export type ListTunneledMcpServerConnectionsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListTunneledMcpServerConnectionsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTunneledMcpServerConnectionsSecurity = {
    option1?: ListTunneledMcpServerConnectionsSecurityOption1 | undefined;
    option2?: ListTunneledMcpServerConnectionsSecurityOption2 | undefined;
};
export type ListTunneledMcpServerConnectionsRequest = {
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
export type ListTunneledMcpServerConnectionsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListTunneledMcpServerConnectionsSecurityOption1$outboundSchema: z.ZodMiniType<ListTunneledMcpServerConnectionsSecurityOption1$Outbound, ListTunneledMcpServerConnectionsSecurityOption1>;
export declare function listTunneledMcpServerConnectionsSecurityOption1ToJSON(listTunneledMcpServerConnectionsSecurityOption1: ListTunneledMcpServerConnectionsSecurityOption1): string;
/** @internal */
export type ListTunneledMcpServerConnectionsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTunneledMcpServerConnectionsSecurityOption2$outboundSchema: z.ZodMiniType<ListTunneledMcpServerConnectionsSecurityOption2$Outbound, ListTunneledMcpServerConnectionsSecurityOption2>;
export declare function listTunneledMcpServerConnectionsSecurityOption2ToJSON(listTunneledMcpServerConnectionsSecurityOption2: ListTunneledMcpServerConnectionsSecurityOption2): string;
/** @internal */
export type ListTunneledMcpServerConnectionsSecurity$Outbound = {
    Option1?: ListTunneledMcpServerConnectionsSecurityOption1$Outbound | undefined;
    Option2?: ListTunneledMcpServerConnectionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListTunneledMcpServerConnectionsSecurity$outboundSchema: z.ZodMiniType<ListTunneledMcpServerConnectionsSecurity$Outbound, ListTunneledMcpServerConnectionsSecurity>;
export declare function listTunneledMcpServerConnectionsSecurityToJSON(listTunneledMcpServerConnectionsSecurity: ListTunneledMcpServerConnectionsSecurity): string;
/** @internal */
export type ListTunneledMcpServerConnectionsRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTunneledMcpServerConnectionsRequest$outboundSchema: z.ZodMiniType<ListTunneledMcpServerConnectionsRequest$Outbound, ListTunneledMcpServerConnectionsRequest>;
export declare function listTunneledMcpServerConnectionsRequestToJSON(listTunneledMcpServerConnectionsRequest: ListTunneledMcpServerConnectionsRequest): string;
//# sourceMappingURL=listtunneledmcpserverconnections.d.ts.map