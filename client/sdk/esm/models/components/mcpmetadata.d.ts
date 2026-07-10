import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { McpEnvironmentConfig } from "./mcpenvironmentconfig.js";
/**
 * Metadata used to configure the MCP install page. Exactly one of toolset_id or mcp_server_id identifies which backend the metadata belongs to.
 */
export type McpMetadata = {
    /**
     * When the metadata entry was created
     */
    createdAt: Date;
    /**
     * The default environment to load variables from
     */
    defaultEnvironmentId?: string | undefined;
    /**
     * The list of environment variables configured for this MCP
     */
    environmentConfigs?: Array<McpEnvironmentConfig> | undefined;
    /**
     * A blob of text for the button on the MCP server page
     */
    externalDocumentationText?: string | undefined;
    /**
     * A link to external documentation for the MCP install page
     */
    externalDocumentationUrl?: string | undefined;
    /**
     * The ID of the metadata record
     */
    id: string;
    /**
     * URL to redirect to instead of showing the default installation page
     */
    installationOverrideUrl?: string | undefined;
    /**
     * Server instructions returned in the MCP initialize response
     */
    instructions?: string | undefined;
    /**
     * The asset ID for the MCP install page logo
     */
    logoAssetId?: string | undefined;
    /**
     * The MCP server associated with this install page metadata. Mutually exclusive with toolset_id.
     */
    mcpServerId?: string | undefined;
    /**
     * The toolset associated with this install page metadata. Mutually exclusive with mcp_server_id.
     */
    toolsetId?: string | undefined;
    /**
     * When the metadata entry was last updated
     */
    updatedAt: Date;
};
/** @internal */
export declare const McpMetadata$inboundSchema: z.ZodMiniType<McpMetadata, unknown>;
export declare function mcpMetadataFromJSON(jsonString: string): SafeParseResult<McpMetadata, SDKValidationError>;
//# sourceMappingURL=mcpmetadata.d.ts.map