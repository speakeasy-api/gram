import * as z from "zod/v4-mini";
export type AddExternalMCPForm = {
  /**
   * The display name for the external MCP server.
   */
  name: string;
  /**
   * The ID of the internal collection registry the server is from.
   */
  organizationMcpCollectionRegistryId?: string | undefined;
  /**
   * The ID of the external MCP registry the server is from.
   */
  registryId?: string | undefined;
  /**
   * The canonical server name used to look up the server in the registry (e.g., 'slack', 'ai.exa/exa').
   */
  registryServerSpecifier: string;
  /**
   * URLs of the remotes to use for this MCP server. If not provided, the backend will auto-select based on transport type preference.
   */
  selectedRemotes?: Array<string> | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export type AddExternalMCPForm$Outbound = {
  name: string;
  organization_mcp_collection_registry_id?: string | undefined;
  registry_id?: string | undefined;
  registry_server_specifier: string;
  selected_remotes?: Array<string> | undefined;
  slug: string;
};
/** @internal */
export declare const AddExternalMCPForm$outboundSchema: z.ZodMiniType<
  AddExternalMCPForm$Outbound,
  AddExternalMCPForm
>;
export declare function addExternalMCPFormToJSON(
  addExternalMCPForm: AddExternalMCPForm,
): string;
//# sourceMappingURL=addexternalmcpform.d.ts.map
