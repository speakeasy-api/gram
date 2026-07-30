import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Stdio-based MCP client configuration (Claude Desktop, Cursor)
 */
export type McpExportStdioConfig = {
  /**
   * Command arguments
   */
  args: Array<string>;
  /**
   * The command to run
   */
  command: string;
  /**
   * Environment variables
   */
  env?:
    | {
        [k: string]: string;
      }
    | undefined;
};
/** @internal */
export declare const McpExportStdioConfig$inboundSchema: z.ZodType<
  McpExportStdioConfig,
  z.ZodTypeDef,
  unknown
>;
export declare function mcpExportStdioConfigFromJSON(
  jsonString: string,
): SafeParseResult<McpExportStdioConfig, SDKValidationError>;
//# sourceMappingURL=mcpexportstdioconfig.d.ts.map
