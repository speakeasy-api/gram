import * as z from "zod/v4-mini";
/**
 * Form for creating a new tunneled MCP server source
 */
export type CreateTunneledMcpServerForm = {
  /**
   * Human-readable display name for the tunneled MCP server
   */
  name: string;
};
/** @internal */
export type CreateTunneledMcpServerForm$Outbound = {
  name: string;
};
/** @internal */
export declare const CreateTunneledMcpServerForm$outboundSchema: z.ZodMiniType<
  CreateTunneledMcpServerForm$Outbound,
  CreateTunneledMcpServerForm
>;
export declare function createTunneledMcpServerFormToJSON(
  createTunneledMcpServerForm: CreateTunneledMcpServerForm,
): string;
//# sourceMappingURL=createtunneledmcpserverform.d.ts.map
