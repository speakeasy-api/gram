import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Represents an environment variable configured for an MCP server.
 */
export type McpEnvironmentConfig = {
    /**
     * When the config was created
     */
    createdAt: Date;
    /**
     * Custom display name for the variable in MCP headers
     */
    headerDisplayName?: string | undefined;
    /**
     * The ID of the environment config
     */
    id: string;
    /**
     * How the variable is provided: 'user', 'system', or 'none'
     */
    providedBy: string;
    /**
     * When the config was last updated
     */
    updatedAt: Date;
    /**
     * The name of the environment variable
     */
    variableName: string;
};
/** @internal */
export declare const McpEnvironmentConfig$inboundSchema: z.ZodMiniType<McpEnvironmentConfig, unknown>;
export declare function mcpEnvironmentConfigFromJSON(jsonString: string): SafeParseResult<McpEnvironmentConfig, SDKValidationError>;
//# sourceMappingURL=mcpenvironmentconfig.d.ts.map