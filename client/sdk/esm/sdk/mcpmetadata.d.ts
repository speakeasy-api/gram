import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { GetMcpMetadataResponseBody } from "../models/components/getmcpmetadataresponsebody.js";
import { McpExport } from "../models/components/mcpexport.js";
import { McpMetadata as McpMetadata$Model } from "../models/components/mcpmetadata.js";
import {
  ExportMcpMetadataRequest,
  ExportMcpMetadataSecurity,
} from "../models/operations/exportmcpmetadata.js";
import {
  GetMcpMetadataRequest,
  GetMcpMetadataSecurity,
} from "../models/operations/getmcpmetadata.js";
import {
  SetMcpMetadataRequest,
  SetMcpMetadataSecurity,
} from "../models/operations/setmcpmetadata.js";
export declare class McpMetadata extends ClientSDK {
  /**
   * exportMcpMetadata mcpMetadata
   *
   * @remarks
   * Export MCP server details as JSON for documentation and integration purposes.
   */
  export(
    request: ExportMcpMetadataRequest,
    security?: ExportMcpMetadataSecurity | undefined,
    options?: RequestOptions,
  ): Promise<McpExport>;
  /**
   * getMcpMetadata mcpMetadata
   *
   * @remarks
   * Fetch the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
   */
  get(
    request?: GetMcpMetadataRequest | undefined,
    security?: GetMcpMetadataSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GetMcpMetadataResponseBody>;
  /**
   * setMcpMetadata mcpMetadata
   *
   * @remarks
   * Create or update the metadata that powers the MCP install page. Exactly one of toolset_slug or mcp_server_id must be provided.
   */
  set(
    request: SetMcpMetadataRequest,
    security?: SetMcpMetadataSecurity | undefined,
    options?: RequestOptions,
  ): Promise<McpMetadata$Model>;
}
//# sourceMappingURL=mcpmetadata.d.ts.map
