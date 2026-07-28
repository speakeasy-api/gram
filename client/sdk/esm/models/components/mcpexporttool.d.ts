import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A tool definition in the MCP export
 */
export type McpExportTool = {
  /**
   * Description of what the tool does
   */
  description: string;
  /**
   * JSON Schema for the tool's input parameters
   */
  inputSchema: any;
  /**
   * The tool name
   */
  name: string;
};
/** @internal */
export declare const McpExportTool$inboundSchema: z.ZodMiniType<
  McpExportTool,
  unknown
>;
export declare function mcpExportToolFromJSON(
  jsonString: string,
): SafeParseResult<McpExportTool, SDKValidationError>;
//# sourceMappingURL=mcpexporttool.d.ts.map
