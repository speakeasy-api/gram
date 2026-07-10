import * as z from "zod/v4-mini";
export type ExportMcpMetadataRequestBody = {
    /**
     * The MCP server slug (from the install URL)
     */
    mcpSlug: string;
};
/** @internal */
export type ExportMcpMetadataRequestBody$Outbound = {
    mcp_slug: string;
};
/** @internal */
export declare const ExportMcpMetadataRequestBody$outboundSchema: z.ZodMiniType<ExportMcpMetadataRequestBody$Outbound, ExportMcpMetadataRequestBody>;
export declare function exportMcpMetadataRequestBodyToJSON(exportMcpMetadataRequestBody: ExportMcpMetadataRequestBody): string;
//# sourceMappingURL=exportmcpmetadatarequestbody.d.ts.map