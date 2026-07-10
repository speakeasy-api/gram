import * as z from "zod/v4-mini";
export type ListMcpEndpointsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListMcpEndpointsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListMcpEndpointsSecurity = {
  option1?: ListMcpEndpointsSecurityOption1 | undefined;
  option2?: ListMcpEndpointsSecurityOption2 | undefined;
};
export type ListMcpEndpointsRequest = {
  /**
   * Optional filter: only return endpoints associated with this MCP server.
   */
  mcpServerId?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type ListMcpEndpointsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListMcpEndpointsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListMcpEndpointsSecurityOption1$Outbound,
  ListMcpEndpointsSecurityOption1
>;
export declare function listMcpEndpointsSecurityOption1ToJSON(
  listMcpEndpointsSecurityOption1: ListMcpEndpointsSecurityOption1,
): string;
/** @internal */
export type ListMcpEndpointsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListMcpEndpointsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListMcpEndpointsSecurityOption2$Outbound,
  ListMcpEndpointsSecurityOption2
>;
export declare function listMcpEndpointsSecurityOption2ToJSON(
  listMcpEndpointsSecurityOption2: ListMcpEndpointsSecurityOption2,
): string;
/** @internal */
export type ListMcpEndpointsSecurity$Outbound = {
  Option1?: ListMcpEndpointsSecurityOption1$Outbound | undefined;
  Option2?: ListMcpEndpointsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListMcpEndpointsSecurity$outboundSchema: z.ZodMiniType<
  ListMcpEndpointsSecurity$Outbound,
  ListMcpEndpointsSecurity
>;
export declare function listMcpEndpointsSecurityToJSON(
  listMcpEndpointsSecurity: ListMcpEndpointsSecurity,
): string;
/** @internal */
export type ListMcpEndpointsRequest$Outbound = {
  mcp_server_id?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListMcpEndpointsRequest$outboundSchema: z.ZodMiniType<
  ListMcpEndpointsRequest$Outbound,
  ListMcpEndpointsRequest
>;
export declare function listMcpEndpointsRequestToJSON(
  listMcpEndpointsRequest: ListMcpEndpointsRequest,
): string;
//# sourceMappingURL=listmcpendpoints.d.ts.map
