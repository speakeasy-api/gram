import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpExportAuthHeader } from "./mcpexportauthheader.js";
/**
 * Authentication requirements for the MCP server
 */
export type McpExportAuthentication = {
    /**
     * Required authentication headers
     */
    headers: Array<McpExportAuthHeader>;
    /**
     * Whether authentication is required
     */
    required: boolean;
};
/** @internal */
export declare const McpExportAuthentication$inboundSchema: z.ZodMiniType<McpExportAuthentication, unknown>;
export declare function mcpExportAuthenticationFromJSON(jsonString: string): SafeParseResult<McpExportAuthentication, SDKValidationError>;
//# sourceMappingURL=mcpexportauthentication.d.ts.map