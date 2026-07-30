import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpExportHTTPConfig } from "./mcpexporthttpconfig.js";
import { McpExportStdioConfig } from "./mcpexportstdioconfig.js";
/**
 * Installation configurations for different MCP clients
 */
export type McpExportInstallConfigs = {
  /**
   * CLI command for Claude Code
   */
  claudeCode: string;
  /**
   * Stdio-based MCP client configuration (Claude Desktop, Cursor)
   */
  claudeDesktop: McpExportStdioConfig;
  /**
   * Stdio-based MCP client configuration (Claude Desktop, Cursor)
   */
  cursor: McpExportStdioConfig;
  /**
   * HTTP-based MCP client configuration (VS Code)
   */
  vscode: McpExportHTTPConfig;
};
/** @internal */
export declare const McpExportInstallConfigs$inboundSchema: z.ZodType<
  McpExportInstallConfigs,
  z.ZodTypeDef,
  unknown
>;
export declare function mcpExportInstallConfigsFromJSON(
  jsonString: string,
): SafeParseResult<McpExportInstallConfigs, SDKValidationError>;
//# sourceMappingURL=mcpexportinstallconfigs.d.ts.map
