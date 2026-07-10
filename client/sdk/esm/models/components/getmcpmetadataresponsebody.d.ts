import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpMetadata } from "./mcpmetadata.js";
export type GetMcpMetadataResponseBody = {
  /**
   * Metadata used to configure the MCP install page. Exactly one of toolset_id or mcp_server_id identifies which backend the metadata belongs to.
   */
  metadata?: McpMetadata | undefined;
};
/** @internal */
export declare const GetMcpMetadataResponseBody$inboundSchema: z.ZodMiniType<
  GetMcpMetadataResponseBody,
  unknown
>;
export declare function getMcpMetadataResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<GetMcpMetadataResponseBody, SDKValidationError>;
//# sourceMappingURL=getmcpmetadataresponsebody.d.ts.map
