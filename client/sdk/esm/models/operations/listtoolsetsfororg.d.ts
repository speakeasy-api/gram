import * as z from "zod/v4-mini";
export type ListToolsetsForOrgSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListToolsetsForOrgRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type ListToolsetsForOrgSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListToolsetsForOrgSecurity$outboundSchema: z.ZodMiniType<
  ListToolsetsForOrgSecurity$Outbound,
  ListToolsetsForOrgSecurity
>;
export declare function listToolsetsForOrgSecurityToJSON(
  listToolsetsForOrgSecurity: ListToolsetsForOrgSecurity,
): string;
/** @internal */
export type ListToolsetsForOrgRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListToolsetsForOrgRequest$outboundSchema: z.ZodMiniType<
  ListToolsetsForOrgRequest$Outbound,
  ListToolsetsForOrgRequest
>;
export declare function listToolsetsForOrgRequestToJSON(
  listToolsetsForOrgRequest: ListToolsetsForOrgRequest,
): string;
//# sourceMappingURL=listtoolsetsfororg.d.ts.map
