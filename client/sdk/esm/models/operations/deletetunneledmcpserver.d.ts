import * as z from "zod/v4-mini";
export type DeleteTunneledMcpServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteTunneledMcpServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteTunneledMcpServerSecurity = {
  option1?: DeleteTunneledMcpServerSecurityOption1 | undefined;
  option2?: DeleteTunneledMcpServerSecurityOption2 | undefined;
};
export type DeleteTunneledMcpServerRequest = {
  /**
   * The ID of the tunneled MCP server to delete
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
export type DeleteTunneledMcpServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteTunneledMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteTunneledMcpServerSecurityOption1$Outbound,
  DeleteTunneledMcpServerSecurityOption1
>;
export declare function deleteTunneledMcpServerSecurityOption1ToJSON(
  deleteTunneledMcpServerSecurityOption1: DeleteTunneledMcpServerSecurityOption1,
): string;
/** @internal */
export type DeleteTunneledMcpServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteTunneledMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteTunneledMcpServerSecurityOption2$Outbound,
  DeleteTunneledMcpServerSecurityOption2
>;
export declare function deleteTunneledMcpServerSecurityOption2ToJSON(
  deleteTunneledMcpServerSecurityOption2: DeleteTunneledMcpServerSecurityOption2,
): string;
/** @internal */
export type DeleteTunneledMcpServerSecurity$Outbound = {
  Option1?: DeleteTunneledMcpServerSecurityOption1$Outbound | undefined;
  Option2?: DeleteTunneledMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteTunneledMcpServerSecurity$outboundSchema: z.ZodMiniType<
  DeleteTunneledMcpServerSecurity$Outbound,
  DeleteTunneledMcpServerSecurity
>;
export declare function deleteTunneledMcpServerSecurityToJSON(
  deleteTunneledMcpServerSecurity: DeleteTunneledMcpServerSecurity,
): string;
/** @internal */
export type DeleteTunneledMcpServerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteTunneledMcpServerRequest$outboundSchema: z.ZodMiniType<
  DeleteTunneledMcpServerRequest$Outbound,
  DeleteTunneledMcpServerRequest
>;
export declare function deleteTunneledMcpServerRequestToJSON(
  deleteTunneledMcpServerRequest: DeleteTunneledMcpServerRequest,
): string;
//# sourceMappingURL=deletetunneledmcpserver.d.ts.map
