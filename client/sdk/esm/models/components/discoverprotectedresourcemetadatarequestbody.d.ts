import * as z from "zod/v4-mini";
export type DiscoverProtectedResourceMetadataRequestBody = {
    /**
     * The ID of the remote MCP server to probe.
     */
    remoteMcpServerId: string;
};
/** @internal */
export type DiscoverProtectedResourceMetadataRequestBody$Outbound = {
    remote_mcp_server_id: string;
};
/** @internal */
export declare const DiscoverProtectedResourceMetadataRequestBody$outboundSchema: z.ZodMiniType<DiscoverProtectedResourceMetadataRequestBody$Outbound, DiscoverProtectedResourceMetadataRequestBody>;
export declare function discoverProtectedResourceMetadataRequestBodyToJSON(discoverProtectedResourceMetadataRequestBody: DiscoverProtectedResourceMetadataRequestBody): string;
//# sourceMappingURL=discoverprotectedresourcemetadatarequestbody.d.ts.map