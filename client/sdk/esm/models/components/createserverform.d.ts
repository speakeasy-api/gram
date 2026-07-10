import * as z from "zod/v4-mini";
import { HeaderInput, HeaderInput$Outbound } from "./headerinput.js";
/**
 * Form for creating a new remote MCP server
 */
export type CreateServerForm = {
  /**
   * Headers to send when proxying requests to the remote server
   */
  headers: Array<HeaderInput>;
  /**
   * Optional human-readable name for the remote MCP server. Empty values are stored as null.
   */
  name?: string | undefined;
  /**
   * The transport type for the remote MCP server (e.g. streamable-http)
   */
  transportType: string;
  /**
   * The URL of the remote MCP server
   */
  url: string;
};
/** @internal */
export type CreateServerForm$Outbound = {
  headers: Array<HeaderInput$Outbound>;
  name?: string | undefined;
  transport_type: string;
  url: string;
};
/** @internal */
export declare const CreateServerForm$outboundSchema: z.ZodMiniType<
  CreateServerForm$Outbound,
  CreateServerForm
>;
export declare function createServerFormToJSON(
  createServerForm: CreateServerForm,
): string;
//# sourceMappingURL=createserverform.d.ts.map
