import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An MCP server attached to a remote_session_client, with the fields the org-admin UI needs to display and link to it.
 */
export type OrganizationMcpServer = {
    /**
     * The mcp_server id.
     */
    id: string;
    /**
     * The MCP server name; empty when unset (display falls back to the URL).
     */
    name?: string | undefined;
    /**
     * The owning project id.
     */
    projectId: string;
    /**
     * The owning project's slug, for linking to the MCP server in its project.
     */
    projectSlug?: string | undefined;
    /**
     * The MCP server slug.
     */
    slug?: string | undefined;
    /**
     * The remote MCP server URL; empty for non-remote (toolset-backed) servers.
     */
    url?: string | undefined;
};
/** @internal */
export declare const OrganizationMcpServer$inboundSchema: z.ZodMiniType<OrganizationMcpServer, unknown>;
export declare function organizationMcpServerFromJSON(jsonString: string): SafeParseResult<OrganizationMcpServer, SDKValidationError>;
//# sourceMappingURL=organizationmcpserver.d.ts.map