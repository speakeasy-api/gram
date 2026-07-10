import * as z from "zod/v4-mini";
/**
 * Form for creating a new MCP endpoint. Platform-domain endpoint slugs (no custom_domain_id) must be prefixed with the organization slug.
 */
export type CreateMcpEndpointForm = {
  /**
   * The ID of the custom domain to register the endpoint slug under. Omit for a platform-domain endpoint.
   */
  customDomainId?: string | undefined;
  /**
   * The ID of the MCP server this endpoint addresses
   */
  mcpServerId: string;
  /**
   * A url-friendly label (up to 128 characters) that addresses an MCP server through a slug-based URL. Platform-domain slugs (no custom domain) must be prefixed with the organization slug.
   */
  slug: string;
};
/** @internal */
export type CreateMcpEndpointForm$Outbound = {
  custom_domain_id?: string | undefined;
  mcp_server_id: string;
  slug: string;
};
/** @internal */
export declare const CreateMcpEndpointForm$outboundSchema: z.ZodMiniType<
  CreateMcpEndpointForm$Outbound,
  CreateMcpEndpointForm
>;
export declare function createMcpEndpointFormToJSON(
  createMcpEndpointForm: CreateMcpEndpointForm,
): string;
//# sourceMappingURL=createmcpendpointform.d.ts.map
