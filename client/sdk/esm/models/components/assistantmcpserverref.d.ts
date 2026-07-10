import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AssistantMCPServerRef = {
  /**
   * The slug of the server's Gram-hosted MCP endpoint (/mcp/{endpoint_slug}). Populated on reads; ignored on writes. Absent when the server has no Gram-hosted endpoint.
   */
  endpointSlug?: string | undefined;
  /**
   * Optional environment slug used when connecting to the MCP server.
   */
  environmentSlug?: string | undefined;
  /**
   * The MCP server slug exposed to the assistant. Covers remote- and tunnelled-backed MCP servers, which have no toolset to attach.
   */
  mcpServerSlug: string;
};
/** @internal */
export declare const AssistantMCPServerRef$inboundSchema: z.ZodMiniType<
  AssistantMCPServerRef,
  unknown
>;
/** @internal */
export type AssistantMCPServerRef$Outbound = {
  endpoint_slug?: string | undefined;
  environment_slug?: string | undefined;
  mcp_server_slug: string;
};
/** @internal */
export declare const AssistantMCPServerRef$outboundSchema: z.ZodMiniType<
  AssistantMCPServerRef$Outbound,
  AssistantMCPServerRef
>;
export declare function assistantMCPServerRefToJSON(
  assistantMCPServerRef: AssistantMCPServerRef,
): string;
export declare function assistantMCPServerRefFromJSON(
  jsonString: string,
): SafeParseResult<AssistantMCPServerRef, SDKValidationError>;
//# sourceMappingURL=assistantmcpserverref.d.ts.map
