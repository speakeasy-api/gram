import * as z from "zod/v4-mini";
export type GetMcpMetadataSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetMcpMetadataSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetMcpMetadataSecurity = {
  option1?: GetMcpMetadataSecurityOption1 | undefined;
  option2?: GetMcpMetadataSecurityOption2 | undefined;
};
export type GetMcpMetadataRequest = {
  /**
   * The slug of the toolset associated with this install page metadata. Mutually exclusive with mcp_server_id.
   */
  toolsetSlug?: string | undefined;
  /**
   * The ID of the MCP server associated with this install page metadata. Mutually exclusive with toolset_slug.
   */
  mcpServerId?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type GetMcpMetadataSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetMcpMetadataSecurityOption1$outboundSchema: z.ZodMiniType<
  GetMcpMetadataSecurityOption1$Outbound,
  GetMcpMetadataSecurityOption1
>;
export declare function getMcpMetadataSecurityOption1ToJSON(
  getMcpMetadataSecurityOption1: GetMcpMetadataSecurityOption1,
): string;
/** @internal */
export type GetMcpMetadataSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetMcpMetadataSecurityOption2$outboundSchema: z.ZodMiniType<
  GetMcpMetadataSecurityOption2$Outbound,
  GetMcpMetadataSecurityOption2
>;
export declare function getMcpMetadataSecurityOption2ToJSON(
  getMcpMetadataSecurityOption2: GetMcpMetadataSecurityOption2,
): string;
/** @internal */
export type GetMcpMetadataSecurity$Outbound = {
  Option1?: GetMcpMetadataSecurityOption1$Outbound | undefined;
  Option2?: GetMcpMetadataSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetMcpMetadataSecurity$outboundSchema: z.ZodMiniType<
  GetMcpMetadataSecurity$Outbound,
  GetMcpMetadataSecurity
>;
export declare function getMcpMetadataSecurityToJSON(
  getMcpMetadataSecurity: GetMcpMetadataSecurity,
): string;
/** @internal */
export type GetMcpMetadataRequest$Outbound = {
  toolset_slug?: string | undefined;
  mcp_server_id?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetMcpMetadataRequest$outboundSchema: z.ZodMiniType<
  GetMcpMetadataRequest$Outbound,
  GetMcpMetadataRequest
>;
export declare function getMcpMetadataRequestToJSON(
  getMcpMetadataRequest: GetMcpMetadataRequest,
): string;
//# sourceMappingURL=getmcpmetadata.d.ts.map
