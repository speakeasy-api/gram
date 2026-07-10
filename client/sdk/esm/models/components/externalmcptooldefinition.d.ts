import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CanonicalToolAttributes } from "./canonicaltoolattributes.js";
import { ToolAnnotations } from "./toolannotations.js";
import { ToolVariation } from "./toolvariation.js";
/**
 * The transport type used to connect to the MCP server
 */
export declare const ExternalMCPToolDefinitionTransportType: {
    readonly StreamableHttp: "streamable-http";
    readonly Sse: "sse";
};
/**
 * The transport type used to connect to the MCP server
 */
export type ExternalMCPToolDefinitionTransportType = ClosedEnum<typeof ExternalMCPToolDefinitionTransportType>;
/**
 * A proxy tool that references an external MCP server
 */
export type ExternalMCPToolDefinition = {
    /**
     * Tool annotations providing behavioral hints about the tool
     */
    annotations?: ToolAnnotations | undefined;
    /**
     * The original details of a tool
     */
    canonical?: CanonicalToolAttributes | undefined;
    /**
     * The canonical name of the tool. Will be the same as the name if there is no variation.
     */
    canonicalName: string;
    /**
     * Confirmation mode for the tool
     */
    confirm?: string | undefined;
    /**
     * Prompt for the confirmation
     */
    confirmPrompt?: string | undefined;
    /**
     * The creation date of the tool.
     */
    createdAt: Date;
    /**
     * The ID of the deployments_external_mcps record
     */
    deploymentExternalMcpId: string;
    /**
     * The ID of the deployment
     */
    deploymentId: string;
    /**
     * Description of the tool
     */
    description: string;
    /**
     * The ID of the tool
     */
    id: string;
    /**
     * The name of the tool
     */
    name: string;
    /**
     * The OAuth authorization endpoint URL
     */
    oauthAuthorizationEndpoint?: string | undefined;
    /**
     * The OAuth dynamic client registration endpoint URL
     */
    oauthRegistrationEndpoint?: string | undefined;
    /**
     * The OAuth scopes supported by the server
     */
    oauthScopesSupported?: Array<string> | undefined;
    /**
     * The OAuth token endpoint URL
     */
    oauthTokenEndpoint?: string | undefined;
    /**
     * OAuth version: '2.1' (MCP OAuth), '2.0' (legacy), or 'none'
     */
    oauthVersion: string;
    /**
     * The ID of the project
     */
    projectId: string;
    /**
     * The ID of the MCP registry
     */
    registryId: string;
    /**
     * The name of the external MCP server (e.g., exa)
     */
    registryServerName: string;
    /**
     * The specifier of the external MCP server (e.g., 'io.modelcontextprotocol.anonymous/exa')
     */
    registrySpecifier: string;
    /**
     * The URL to connect to the MCP server
     */
    remoteUrl: string;
    /**
     * Whether the external MCP server requires OAuth authentication
     */
    requiresOauth: boolean;
    /**
     * JSON schema for the request
     */
    schema: string;
    /**
     * Version of the schema
     */
    schemaVersion?: string | undefined;
    /**
     * The slug used for tool prefixing (e.g., github)
     */
    slug: string;
    /**
     * Summarizer for the tool
     */
    summarizer?: string | undefined;
    /**
     * The URN of this tool
     */
    toolUrn: string;
    /**
     * The transport type used to connect to the MCP server
     */
    transportType: ExternalMCPToolDefinitionTransportType;
    /**
     * Whether or not the tool is a proxy tool
     */
    type?: string | undefined;
    /**
     * The last update date of the tool.
     */
    updatedAt: Date;
    variation?: ToolVariation | undefined;
};
/** @internal */
export declare const ExternalMCPToolDefinitionTransportType$inboundSchema: z.ZodMiniEnum<typeof ExternalMCPToolDefinitionTransportType>;
/** @internal */
export declare const ExternalMCPToolDefinition$inboundSchema: z.ZodMiniType<ExternalMCPToolDefinition, unknown>;
export declare function externalMCPToolDefinitionFromJSON(jsonString: string): SafeParseResult<ExternalMCPToolDefinition, SDKValidationError>;
//# sourceMappingURL=externalmcptooldefinition.d.ts.map