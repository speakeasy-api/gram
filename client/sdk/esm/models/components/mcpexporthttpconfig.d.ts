import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * HTTP-based MCP client configuration (VS Code)
 */
export type McpExportHTTPConfig = {
  /**
   * HTTP headers with environment variable placeholders
   */
  headers?:
    | {
        [k: string]: string;
      }
    | undefined;
  /**
   * Transport type (always 'http')
   */
  type: string;
  /**
   * The MCP server URL
   */
  url: string;
};
/** @internal */
export declare const McpExportHTTPConfig$inboundSchema: z.ZodType<
  McpExportHTTPConfig,
  z.ZodTypeDef,
  unknown
>;
export declare function mcpExportHTTPConfigFromJSON(
  jsonString: string,
): SafeParseResult<McpExportHTTPConfig, SDKValidationError>;
//# sourceMappingURL=mcpexporthttpconfig.d.ts.map
