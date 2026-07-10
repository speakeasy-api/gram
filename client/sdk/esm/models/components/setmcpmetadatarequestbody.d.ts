import * as z from "zod/v4-mini";
import { McpEnvironmentConfigInput, McpEnvironmentConfigInput$Outbound } from "./mcpenvironmentconfiginput.js";
export type SetMcpMetadataRequestBody = {
    /**
     * The default environment to load variables from. Not supported when mcp_server_id is provided.
     */
    defaultEnvironmentId?: string | undefined;
    /**
     * The list of environment variables to configure for this MCP
     */
    environmentConfigs?: Array<McpEnvironmentConfigInput> | undefined;
    /**
     * A blob of text for the button on the MCP server page
     */
    externalDocumentationText?: string | undefined;
    /**
     * A link to external documentation for the MCP install page
     */
    externalDocumentationUrl?: string | undefined;
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
     * The ID of the MCP server associated with this install page metadata. Mutually exclusive with toolset_slug.
     */
    mcpServerId?: string | undefined;
    /**
     * The slug of the toolset associated with this install page metadata. Mutually exclusive with mcp_server_id.
     */
    toolsetSlug?: string | undefined;
};
/** @internal */
export type SetMcpMetadataRequestBody$Outbound = {
    default_environment_id?: string | undefined;
    environment_configs?: Array<McpEnvironmentConfigInput$Outbound> | undefined;
    external_documentation_text?: string | undefined;
    external_documentation_url?: string | undefined;
    installation_override_url?: string | undefined;
    instructions?: string | undefined;
    logo_asset_id?: string | undefined;
    mcp_server_id?: string | undefined;
    toolset_slug?: string | undefined;
};
/** @internal */
export declare const SetMcpMetadataRequestBody$outboundSchema: z.ZodMiniType<SetMcpMetadataRequestBody$Outbound, SetMcpMetadataRequestBody>;
export declare function setMcpMetadataRequestBodyToJSON(setMcpMetadataRequestBody: SetMcpMetadataRequestBody): string;
//# sourceMappingURL=setmcpmetadatarequestbody.d.ts.map