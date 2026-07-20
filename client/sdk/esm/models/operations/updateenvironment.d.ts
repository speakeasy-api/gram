import * as z from "zod/v4-mini";
import {
  UpdateEnvironmentRequestBody,
  UpdateEnvironmentRequestBody$Outbound,
} from "../components/updateenvironmentrequestbody.js";
export type UpdateEnvironmentSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateEnvironmentRequest = {
  /**
   * The slug of the environment to update
   */
  slug: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updateEnvironmentRequestBody: UpdateEnvironmentRequestBody;
};
/** @internal */
export type UpdateEnvironmentSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateEnvironmentSecurity$outboundSchema: z.ZodMiniType<
  UpdateEnvironmentSecurity$Outbound,
  UpdateEnvironmentSecurity
>;
export declare function updateEnvironmentSecurityToJSON(
  updateEnvironmentSecurity: UpdateEnvironmentSecurity,
): string;
/** @internal */
export type UpdateEnvironmentRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateEnvironmentRequestBody: UpdateEnvironmentRequestBody$Outbound;
};
/** @internal */
export declare const UpdateEnvironmentRequest$outboundSchema: z.ZodMiniType<
  UpdateEnvironmentRequest$Outbound,
  UpdateEnvironmentRequest
>;
export declare function updateEnvironmentRequestToJSON(
  updateEnvironmentRequest: UpdateEnvironmentRequest,
): string;
//# sourceMappingURL=updateenvironment.d.ts.map
