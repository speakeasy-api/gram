import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type CreateResponseSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  projectSlugHeaderGramProject?: string | undefined;
};
export type CreateResponseRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  workflowAgentRequest: components.WorkflowAgentRequest;
};
/** @internal */
export type CreateResponseSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CreateResponseSecurity$outboundSchema: z.ZodMiniType<
  CreateResponseSecurity$Outbound,
  CreateResponseSecurity
>;
export declare function createResponseSecurityToJSON(
  createResponseSecurity: CreateResponseSecurity,
): string;
/** @internal */
export type CreateResponseRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  WorkflowAgentRequest: components.WorkflowAgentRequest$Outbound;
};
/** @internal */
export declare const CreateResponseRequest$outboundSchema: z.ZodMiniType<
  CreateResponseRequest$Outbound,
  CreateResponseRequest
>;
export declare function createResponseRequestToJSON(
  createResponseRequest: CreateResponseRequest,
): string;
//# sourceMappingURL=createresponse.d.ts.map
