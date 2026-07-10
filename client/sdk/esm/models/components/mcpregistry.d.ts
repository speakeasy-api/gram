import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An MCP registry
 */
export type MCPRegistry = {
    /**
     * Registry ID
     */
    id: string;
    /**
     * Display name for the registry
     */
    name: string;
    /**
     * URL of the registry
     */
    url: string;
};
/** @internal */
export declare const MCPRegistry$inboundSchema: z.ZodMiniType<MCPRegistry, unknown>;
export declare function mcpRegistryFromJSON(jsonString: string): SafeParseResult<MCPRegistry, SDKValidationError>;
//# sourceMappingURL=mcpregistry.d.ts.map