import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpExportAuthentication } from "./mcpexportauthentication.js";
import { McpExportTool } from "./mcpexporttool.js";
/**
 * Complete MCP server export for documentation and integration
 */
export type McpExport = {
  /**
   * Authentication requirements for the MCP server
   */
  authentication: McpExportAuthentication;
  /**
   * Description of the MCP server
   */
  description?: string | undefined;
  /**
   * Link to external documentation
   */
  documentationUrl?: string | undefined;
  /**
   * Server instructions for users
   */
  instructions?: string | undefined;
  /**
   * URL to the server logo
   */
  logoUrl?: string | undefined;
  /**
   * The MCP server name
   */
  name: string;
  /**
   * The MCP server URL
   */
  serverUrl: string;
  /**
   * The MCP server slug
   */
  slug: string;
  /**
   * Available tools on this MCP server
   */
  tools: Array<McpExportTool>;
};
/** @internal */
export declare const McpExport$inboundSchema: z.ZodMiniType<McpExport, unknown>;
export declare function mcpExportFromJSON(
  jsonString: string,
): SafeParseResult<McpExport, SDKValidationError>;
//# sourceMappingURL=mcpexport.d.ts.map
