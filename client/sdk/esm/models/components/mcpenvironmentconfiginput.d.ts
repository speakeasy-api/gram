import * as z from "zod/v4-mini";
/**
 * Input for configuring an environment variable for an MCP server.
 */
export type McpEnvironmentConfigInput = {
  /**
   * Custom display name for the variable in MCP headers
   */
  headerDisplayName?: string | undefined;
  /**
   * How the variable is provided: 'user', 'system', or 'none'
   */
  providedBy: string;
  /**
   * The name of the environment variable
   */
  variableName: string;
};
/** @internal */
export type McpEnvironmentConfigInput$Outbound = {
  header_display_name?: string | undefined;
  provided_by: string;
  variable_name: string;
};
/** @internal */
export declare const McpEnvironmentConfigInput$outboundSchema: z.ZodMiniType<
  McpEnvironmentConfigInput$Outbound,
  McpEnvironmentConfigInput
>;
export declare function mcpEnvironmentConfigInputToJSON(
  mcpEnvironmentConfigInput: McpEnvironmentConfigInput,
): string;
//# sourceMappingURL=mcpenvironmentconfiginput.d.ts.map
