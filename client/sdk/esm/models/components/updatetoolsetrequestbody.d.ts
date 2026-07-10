import * as z from "zod/v4-mini";
export type UpdateToolsetRequestBody = {
  /**
   * The ID of the custom domain to use for the toolset
   */
  customDomainId?: string | undefined;
  /**
   * The slug of the environment to use as the default for the toolset
   */
  defaultEnvironmentSlug?: string | undefined;
  /**
   * The new description of the toolset
   */
  description?: string | undefined;
  /**
   * Whether the toolset is enabled for MCP
   */
  mcpEnabled?: boolean | undefined;
  /**
   * Whether the toolset is public in MCP
   */
  mcpIsPublic?: boolean | undefined;
  /**
   * The slug of the MCP to use for the toolset
   */
  mcpSlug?: string | undefined;
  /**
   * The new name of the toolset
   */
  name?: string | undefined;
  /**
   * List of prompt template names to include (note: for actual prompts, not tools)
   */
  promptTemplateNames?: Array<string> | undefined;
  /**
   * List of resource URNs to include in the toolset
   */
  resourceUrns?: Array<string> | undefined;
  /**
   * The mode to use for tool selection
   */
  toolSelectionMode?: string | undefined;
  /**
   * List of tool URNs to include in the toolset
   */
  toolUrns?: Array<string> | undefined;
};
/** @internal */
export type UpdateToolsetRequestBody$Outbound = {
  custom_domain_id?: string | undefined;
  default_environment_slug?: string | undefined;
  description?: string | undefined;
  mcp_enabled?: boolean | undefined;
  mcp_is_public?: boolean | undefined;
  mcp_slug?: string | undefined;
  name?: string | undefined;
  prompt_template_names?: Array<string> | undefined;
  resource_urns?: Array<string> | undefined;
  tool_selection_mode?: string | undefined;
  tool_urns?: Array<string> | undefined;
};
/** @internal */
export declare const UpdateToolsetRequestBody$outboundSchema: z.ZodMiniType<
  UpdateToolsetRequestBody$Outbound,
  UpdateToolsetRequestBody
>;
export declare function updateToolsetRequestBodyToJSON(
  updateToolsetRequestBody: UpdateToolsetRequestBody,
): string;
//# sourceMappingURL=updatetoolsetrequestbody.d.ts.map
