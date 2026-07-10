import * as z from "zod/v4-mini";
import {
  CreateEnvironmentForm,
  CreateEnvironmentForm$Outbound,
} from "../components/createenvironmentform.js";
export type CreateEnvironmentSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type CreateEnvironmentRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  createEnvironmentForm: CreateEnvironmentForm;
};
/** @internal */
export type CreateEnvironmentSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateEnvironmentSecurity$outboundSchema: z.ZodMiniType<
  CreateEnvironmentSecurity$Outbound,
  CreateEnvironmentSecurity
>;
export declare function createEnvironmentSecurityToJSON(
  createEnvironmentSecurity: CreateEnvironmentSecurity,
): string;
/** @internal */
export type CreateEnvironmentRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateEnvironmentForm: CreateEnvironmentForm$Outbound;
};
/** @internal */
export declare const CreateEnvironmentRequest$outboundSchema: z.ZodMiniType<
  CreateEnvironmentRequest$Outbound,
  CreateEnvironmentRequest
>;
export declare function createEnvironmentRequestToJSON(
  createEnvironmentRequest: CreateEnvironmentRequest,
): string;
//# sourceMappingURL=createenvironment.d.ts.map
