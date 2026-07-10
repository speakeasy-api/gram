import * as z from "zod/v4-mini";
export type MintUserSessionRequestBody = {
    /**
     * Bind the JWT to this remote MCP server's user_session_issuer audience (the /x/mcp convention, since remote servers have no toolset). Mutually exclusive with toolset_id; exactly one must be set. Must be issuer-gated and live in the caller's project.
     */
    mcpServerId?: string | undefined;
    /**
     * Bind the JWT to this toolset's /mcp/{slug} audience. Mutually exclusive with mcp_server_id; exactly one must be set. Must be issuer-gated and live in the caller's project.
     */
    toolsetId?: string | undefined;
};
/** @internal */
export type MintUserSessionRequestBody$Outbound = {
    mcp_server_id?: string | undefined;
    toolset_id?: string | undefined;
};
/** @internal */
export declare const MintUserSessionRequestBody$outboundSchema: z.ZodMiniType<MintUserSessionRequestBody$Outbound, MintUserSessionRequestBody>;
export declare function mintUserSessionRequestBodyToJSON(mintUserSessionRequestBody: MintUserSessionRequestBody): string;
//# sourceMappingURL=mintusersessionrequestbody.d.ts.map