import * as z from "zod/v4-mini";
export type ListMcpServerToolFiltersSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListMcpServerToolFiltersSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListMcpServerToolFiltersSecurity = {
  option1?: ListMcpServerToolFiltersSecurityOption1 | undefined;
  option2?: ListMcpServerToolFiltersSecurityOption2 | undefined;
};
export type ListMcpServerToolFiltersRequest = {
  /**
   * The ID of the MCP server. Mutually exclusive with slug.
   */
  id?: string | undefined;
  /**
   * The slug of the MCP server. Mutually exclusive with id.
   */
  slug?: string | undefined;
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
export type ListMcpServerToolFiltersSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListMcpServerToolFiltersSecurityOption1$outboundSchema: z.ZodMiniType<
  ListMcpServerToolFiltersSecurityOption1$Outbound,
  ListMcpServerToolFiltersSecurityOption1
>;
export declare function listMcpServerToolFiltersSecurityOption1ToJSON(
  listMcpServerToolFiltersSecurityOption1: ListMcpServerToolFiltersSecurityOption1,
): string;
/** @internal */
export type ListMcpServerToolFiltersSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListMcpServerToolFiltersSecurityOption2$outboundSchema: z.ZodMiniType<
  ListMcpServerToolFiltersSecurityOption2$Outbound,
  ListMcpServerToolFiltersSecurityOption2
>;
export declare function listMcpServerToolFiltersSecurityOption2ToJSON(
  listMcpServerToolFiltersSecurityOption2: ListMcpServerToolFiltersSecurityOption2,
): string;
/** @internal */
export type ListMcpServerToolFiltersSecurity$Outbound = {
  Option1?: ListMcpServerToolFiltersSecurityOption1$Outbound | undefined;
  Option2?: ListMcpServerToolFiltersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListMcpServerToolFiltersSecurity$outboundSchema: z.ZodMiniType<
  ListMcpServerToolFiltersSecurity$Outbound,
  ListMcpServerToolFiltersSecurity
>;
export declare function listMcpServerToolFiltersSecurityToJSON(
  listMcpServerToolFiltersSecurity: ListMcpServerToolFiltersSecurity,
): string;
/** @internal */
export type ListMcpServerToolFiltersRequest$Outbound = {
  id?: string | undefined;
  slug?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListMcpServerToolFiltersRequest$outboundSchema: z.ZodMiniType<
  ListMcpServerToolFiltersRequest$Outbound,
  ListMcpServerToolFiltersRequest
>;
export declare function listMcpServerToolFiltersRequestToJSON(
  listMcpServerToolFiltersRequest: ListMcpServerToolFiltersRequest,
): string;
//# sourceMappingURL=listmcpservertoolfilters.d.ts.map
