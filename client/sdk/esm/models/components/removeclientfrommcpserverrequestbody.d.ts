import * as z from "zod/v4-mini";
export type RemoveClientFromMcpServerRequestBody = {
    /**
     * The remote_session_client id.
     */
    clientId: string;
    /**
     * The mcp_server id to detach from.
     */
    mcpServerId: string;
};
/** @internal */
export type RemoveClientFromMcpServerRequestBody$Outbound = {
    client_id: string;
    mcp_server_id: string;
};
/** @internal */
export declare const RemoveClientFromMcpServerRequestBody$outboundSchema: z.ZodMiniType<RemoveClientFromMcpServerRequestBody$Outbound, RemoveClientFromMcpServerRequestBody>;
export declare function removeClientFromMcpServerRequestBodyToJSON(removeClientFromMcpServerRequestBody: RemoveClientFromMcpServerRequestBody): string;
//# sourceMappingURL=removeclientfrommcpserverrequestbody.d.ts.map