import * as z from "zod/v3";
import * as components from "../components/index.js";
export type CreateAgentResponseSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  projectSlugHeaderGramProject?: string | undefined;
};
export type CreateAgentResponseRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  agentResponseRequest: components.AgentResponseRequest;
};
/** @internal */
export type CreateAgentResponseSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CreateAgentResponseSecurity$outboundSchema: z.ZodType<
  CreateAgentResponseSecurity$Outbound,
  z.ZodTypeDef,
  CreateAgentResponseSecurity
>;
export declare function createAgentResponseSecurityToJSON(
  createAgentResponseSecurity: CreateAgentResponseSecurity,
): string;
/** @internal */
export type CreateAgentResponseRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  AgentResponseRequest: components.AgentResponseRequest$Outbound;
};
/** @internal */
export declare const CreateAgentResponseRequest$outboundSchema: z.ZodType<
  CreateAgentResponseRequest$Outbound,
  z.ZodTypeDef,
  CreateAgentResponseRequest
>;
export declare function createAgentResponseRequestToJSON(
  createAgentResponseRequest: CreateAgentResponseRequest,
): string;
//# sourceMappingURL=createagentresponse.d.ts.map
