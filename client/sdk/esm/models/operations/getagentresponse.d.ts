import * as z from "zod/v3";
export type GetAgentResponseSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  projectSlugHeaderGramProject?: string | undefined;
};
export type GetAgentResponseRequest = {
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
export type GetAgentResponseSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetAgentResponseSecurity$outboundSchema: z.ZodType<
  GetAgentResponseSecurity$Outbound,
  z.ZodTypeDef,
  GetAgentResponseSecurity
>;
export declare function getAgentResponseSecurityToJSON(
  getAgentResponseSecurity: GetAgentResponseSecurity,
): string;
/** @internal */
export type GetAgentResponseRequest$Outbound = {
  response_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetAgentResponseRequest$outboundSchema: z.ZodType<
  GetAgentResponseRequest$Outbound,
  z.ZodTypeDef,
  GetAgentResponseRequest
>;
export declare function getAgentResponseRequestToJSON(
  getAgentResponseRequest: GetAgentResponseRequest,
): string;
//# sourceMappingURL=getagentresponse.d.ts.map
