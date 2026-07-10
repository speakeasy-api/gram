import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An authentication header required by the MCP server
 */
export type McpExportAuthHeader = {
    /**
     * User-friendly display name (e.g., API Key)
     */
    displayName: string;
    /**
     * The HTTP header name (e.g., Authorization)
     */
    name: string;
};
/** @internal */
export declare const McpExportAuthHeader$inboundSchema: z.ZodMiniType<McpExportAuthHeader, unknown>;
export declare function mcpExportAuthHeaderFromJSON(jsonString: string): SafeParseResult<McpExportAuthHeader, SDKValidationError>;
//# sourceMappingURL=mcpexportauthheader.d.ts.map