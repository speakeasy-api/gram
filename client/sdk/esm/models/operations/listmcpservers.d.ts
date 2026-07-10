import * as z from "zod/v4-mini";
export type ListMcpServersSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListMcpServersSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListMcpServersSecurity = {
    option1?: ListMcpServersSecurityOption1 | undefined;
    option2?: ListMcpServersSecurityOption2 | undefined;
};
export type ListMcpServersRequest = {
    /**
     * Filter to MCP servers backed by this remote MCP server
     */
    remoteMcpServerId?: string | undefined;
    /**
     * Filter to MCP servers backed by this tunneled MCP server
     */
    tunneledMcpServerId?: string | undefined;
    /**
     * Filter to MCP servers backed by this toolset
     */
    toolsetId?: string | undefined;
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
export type ListMcpServersSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListMcpServersSecurityOption1$outboundSchema: z.ZodMiniType<ListMcpServersSecurityOption1$Outbound, ListMcpServersSecurityOption1>;
export declare function listMcpServersSecurityOption1ToJSON(listMcpServersSecurityOption1: ListMcpServersSecurityOption1): string;
/** @internal */
export type ListMcpServersSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListMcpServersSecurityOption2$outboundSchema: z.ZodMiniType<ListMcpServersSecurityOption2$Outbound, ListMcpServersSecurityOption2>;
export declare function listMcpServersSecurityOption2ToJSON(listMcpServersSecurityOption2: ListMcpServersSecurityOption2): string;
/** @internal */
export type ListMcpServersSecurity$Outbound = {
    Option1?: ListMcpServersSecurityOption1$Outbound | undefined;
    Option2?: ListMcpServersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListMcpServersSecurity$outboundSchema: z.ZodMiniType<ListMcpServersSecurity$Outbound, ListMcpServersSecurity>;
export declare function listMcpServersSecurityToJSON(listMcpServersSecurity: ListMcpServersSecurity): string;
/** @internal */
export type ListMcpServersRequest$Outbound = {
    remote_mcp_server_id?: string | undefined;
    tunneled_mcp_server_id?: string | undefined;
    toolset_id?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListMcpServersRequest$outboundSchema: z.ZodMiniType<ListMcpServersRequest$Outbound, ListMcpServersRequest>;
export declare function listMcpServersRequestToJSON(listMcpServersRequest: ListMcpServersRequest): string;
//# sourceMappingURL=listmcpservers.d.ts.map