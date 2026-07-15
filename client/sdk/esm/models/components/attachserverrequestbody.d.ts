import * as z from "zod/v4-mini";
export type AttachServerRequestBody = {
  /**
   * ID of the collection
   */
  collectionId: string;
  /**
   * ID of the MCP server to attach (provide exactly one of toolset_id or mcp_server_id)
   */
  mcpServerId?: string | undefined;
  /**
   * ID of the toolset to attach (provide exactly one of toolset_id or mcp_server_id)
   */
  toolsetId?: string | undefined;
};
/** @internal */
export type AttachServerRequestBody$Outbound = {
  collection_id: string;
  mcp_server_id?: string | undefined;
  toolset_id?: string | undefined;
};
/** @internal */
export declare const AttachServerRequestBody$outboundSchema: z.ZodMiniType<
  AttachServerRequestBody$Outbound,
  AttachServerRequestBody
>;
export declare function attachServerRequestBodyToJSON(
  attachServerRequestBody: AttachServerRequestBody,
): string;
//# sourceMappingURL=attachserverrequestbody.d.ts.map
