import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An MCP endpoint: a url-friendly slug identifier that addresses an MCP server.
 */
export type McpEndpoint = {
  /**
   * When the MCP endpoint was created
   */
  createdAt: Date;
  /**
   * The ID of the custom domain this endpoint slug is registered under. Null for platform-domain endpoints.
   */
  customDomainId?: string | undefined;
  /**
   * The ID of the MCP endpoint
   */
  id: string;
  /**
   * The ID of the MCP server this endpoint addresses
   */
  mcpServerId: string;
  /**
   * The project ID this MCP endpoint belongs to
   */
  projectId: string;
  /**
   * A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug.
   */
  slug: string;
  /**
   * When the MCP endpoint was last updated
   */
  updatedAt: Date;
};
/** @internal */
export declare const McpEndpoint$inboundSchema: z.ZodMiniType<
  McpEndpoint,
  unknown
>;
export declare function mcpEndpointFromJSON(
  jsonString: string,
): SafeParseResult<McpEndpoint, SDKValidationError>;
//# sourceMappingURL=mcpendpoint.d.ts.map
