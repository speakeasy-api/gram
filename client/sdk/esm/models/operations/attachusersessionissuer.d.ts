import * as z from "zod/v4-mini";
import {
  AttachUserSessionIssuerForm,
  AttachUserSessionIssuerForm$Outbound,
} from "../components/attachusersessionissuerform.js";
export type AttachUserSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type AttachUserSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type AttachUserSessionIssuerSecurity = {
  option1?: AttachUserSessionIssuerSecurityOption1 | undefined;
  option2?: AttachUserSessionIssuerSecurityOption2 | undefined;
};
export type AttachUserSessionIssuerRequest = {
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
  attachUserSessionIssuerForm: AttachUserSessionIssuerForm;
};
/** @internal */
export type AttachUserSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const AttachUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  AttachUserSessionIssuerSecurityOption1$Outbound,
  AttachUserSessionIssuerSecurityOption1
>;
export declare function attachUserSessionIssuerSecurityOption1ToJSON(
  attachUserSessionIssuerSecurityOption1: AttachUserSessionIssuerSecurityOption1,
): string;
/** @internal */
export type AttachUserSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const AttachUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  AttachUserSessionIssuerSecurityOption2$Outbound,
  AttachUserSessionIssuerSecurityOption2
>;
export declare function attachUserSessionIssuerSecurityOption2ToJSON(
  attachUserSessionIssuerSecurityOption2: AttachUserSessionIssuerSecurityOption2,
): string;
/** @internal */
export type AttachUserSessionIssuerSecurity$Outbound = {
  Option1?: AttachUserSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: AttachUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const AttachUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  AttachUserSessionIssuerSecurity$Outbound,
  AttachUserSessionIssuerSecurity
>;
export declare function attachUserSessionIssuerSecurityToJSON(
  attachUserSessionIssuerSecurity: AttachUserSessionIssuerSecurity,
): string;
/** @internal */
export type AttachUserSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  AttachUserSessionIssuerForm: AttachUserSessionIssuerForm$Outbound;
};
/** @internal */
export declare const AttachUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  AttachUserSessionIssuerRequest$Outbound,
  AttachUserSessionIssuerRequest
>;
export declare function attachUserSessionIssuerRequestToJSON(
  attachUserSessionIssuerRequest: AttachUserSessionIssuerRequest,
): string;
//# sourceMappingURL=attachusersessionissuer.d.ts.map
