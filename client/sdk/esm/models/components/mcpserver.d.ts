import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The visibility of an MCP server
 */
export declare const McpServerVisibility: {
    readonly Disabled: "disabled";
    readonly Private: "private";
    readonly Public: "public";
};
/**
 * The visibility of an MCP server
 */
export type McpServerVisibility = ClosedEnum<typeof McpServerVisibility>;
/**
 * An MCP server configuration: authentication, environment, and backend selection for an MCP server.
 */
export type McpServer = {
    /**
     * When the MCP server was created
     */
    createdAt: Date;
    /**
     * The ID of the environment associated with the server
     */
    environmentId?: string | undefined;
    /**
     * The ID of the MCP server
     */
    id: string;
    /**
     * A human-readable display name for the server
     */
    name?: string | undefined;
    /**
     * The project ID this MCP server belongs to
     */
    projectId: string;
    /**
     * The ID of the remote MCP server used as the backend
     */
    remoteMcpServerId?: string | undefined;
    /**
     * A URL-safe, project-unique slug derived server-side from the name and ID
     */
    slug?: string | undefined;
    /**
     * The ID of the tool variations group enabling MCP tool filtering for this server, if any.
     */
    toolVariationsGroupId?: string | undefined;
    /**
     * The ID of the toolset used as the backend
     */
    toolsetId?: string | undefined;
    /**
     * The ID of the tunneled MCP server used as the backend
     */
    tunneledMcpServerId?: string | undefined;
    /**
     * When the MCP server was last updated
     */
    updatedAt: Date;
    /**
     * The ID of the user session issuer that gates OAuth-based MCP client authentication for this server, if any.
     */
    userSessionIssuerId?: string | undefined;
    /**
     * The visibility of an MCP server
     */
    visibility: McpServerVisibility;
};
/** @internal */
export declare const McpServerVisibility$inboundSchema: z.ZodMiniEnum<typeof McpServerVisibility>;
/** @internal */
export declare const McpServer$inboundSchema: z.ZodMiniType<McpServer, unknown>;
export declare function mcpServerFromJSON(jsonString: string): SafeParseResult<McpServer, SDKValidationError>;
//# sourceMappingURL=mcpserver.d.ts.map