import * as z from "zod/v4-mini";
import {
  CreateTriggerInstanceForm,
  CreateTriggerInstanceForm$Outbound,
} from "../components/createtriggerinstanceform.js";
export type CreateTriggerInstanceSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type CreateTriggerInstanceRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  createTriggerInstanceForm: CreateTriggerInstanceForm;
};
/** @internal */
export type CreateTriggerInstanceSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<
  CreateTriggerInstanceSecurity$Outbound,
  CreateTriggerInstanceSecurity
>;
export declare function createTriggerInstanceSecurityToJSON(
  createTriggerInstanceSecurity: CreateTriggerInstanceSecurity,
): string;
/** @internal */
export type CreateTriggerInstanceRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateTriggerInstanceForm: CreateTriggerInstanceForm$Outbound;
};
/** @internal */
export declare const CreateTriggerInstanceRequest$outboundSchema: z.ZodMiniType<
  CreateTriggerInstanceRequest$Outbound,
  CreateTriggerInstanceRequest
>;
export declare function createTriggerInstanceRequestToJSON(
  createTriggerInstanceRequest: CreateTriggerInstanceRequest,
): string;
//# sourceMappingURL=createtriggerinstance.d.ts.map
