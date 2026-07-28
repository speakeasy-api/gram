import * as z from "zod/v4-mini";
export type DeleteMcpServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteMcpServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteMcpServerSecurity = {
  option1?: DeleteMcpServerSecurityOption1 | undefined;
  option2?: DeleteMcpServerSecurityOption2 | undefined;
};
export type DeleteMcpServerRequest = {
  /**
   * The ID of the MCP server to delete
   */
  id: string;
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
export type DeleteMcpServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteMcpServerSecurityOption1$Outbound,
  DeleteMcpServerSecurityOption1
>;
export declare function deleteMcpServerSecurityOption1ToJSON(
  deleteMcpServerSecurityOption1: DeleteMcpServerSecurityOption1,
): string;
/** @internal */
export type DeleteMcpServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteMcpServerSecurityOption2$Outbound,
  DeleteMcpServerSecurityOption2
>;
export declare function deleteMcpServerSecurityOption2ToJSON(
  deleteMcpServerSecurityOption2: DeleteMcpServerSecurityOption2,
): string;
/** @internal */
export type DeleteMcpServerSecurity$Outbound = {
  Option1?: DeleteMcpServerSecurityOption1$Outbound | undefined;
  Option2?: DeleteMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteMcpServerSecurity$outboundSchema: z.ZodMiniType<
  DeleteMcpServerSecurity$Outbound,
  DeleteMcpServerSecurity
>;
export declare function deleteMcpServerSecurityToJSON(
  deleteMcpServerSecurity: DeleteMcpServerSecurity,
): string;
/** @internal */
export type DeleteMcpServerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteMcpServerRequest$outboundSchema: z.ZodMiniType<
  DeleteMcpServerRequest$Outbound,
  DeleteMcpServerRequest
>;
export declare function deleteMcpServerRequestToJSON(
  deleteMcpServerRequest: DeleteMcpServerRequest,
): string;
//# sourceMappingURL=deletemcpserver.d.ts.map
