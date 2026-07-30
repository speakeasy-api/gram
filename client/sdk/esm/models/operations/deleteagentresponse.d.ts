import * as z from "zod/v3";
export type DeleteAgentResponseSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  projectSlugHeaderGramProject?: string | undefined;
};
export type DeleteAgentResponseRequest = {
  /**
   * The ID of the response to retrieve
   */
  responseId: string;
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
export type DeleteAgentResponseSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteAgentResponseSecurity$outboundSchema: z.ZodType<
  DeleteAgentResponseSecurity$Outbound,
  z.ZodTypeDef,
  DeleteAgentResponseSecurity
>;
export declare function deleteAgentResponseSecurityToJSON(
  deleteAgentResponseSecurity: DeleteAgentResponseSecurity,
): string;
/** @internal */
export type DeleteAgentResponseRequest$Outbound = {
  response_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteAgentResponseRequest$outboundSchema: z.ZodType<
  DeleteAgentResponseRequest$Outbound,
  z.ZodTypeDef,
  DeleteAgentResponseRequest
>;
export declare function deleteAgentResponseRequestToJSON(
  deleteAgentResponseRequest: DeleteAgentResponseRequest,
): string;
//# sourceMappingURL=deleteagentresponse.d.ts.map
