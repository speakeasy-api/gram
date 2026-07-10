import * as z from "zod/v4-mini";
import {
  AttachUserSessionIssuerForm,
  AttachUserSessionIssuerForm$Outbound,
} from "../components/attachusersessionissuerform.js";
export type DetachUserSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DetachUserSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DetachUserSessionIssuerSecurity = {
  option1?: DetachUserSessionIssuerSecurityOption1 | undefined;
  option2?: DetachUserSessionIssuerSecurityOption2 | undefined;
};
export type DetachUserSessionIssuerRequest = {
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
export type DetachUserSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DetachUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  DetachUserSessionIssuerSecurityOption1$Outbound,
  DetachUserSessionIssuerSecurityOption1
>;
export declare function detachUserSessionIssuerSecurityOption1ToJSON(
  detachUserSessionIssuerSecurityOption1: DetachUserSessionIssuerSecurityOption1,
): string;
/** @internal */
export type DetachUserSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DetachUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  DetachUserSessionIssuerSecurityOption2$Outbound,
  DetachUserSessionIssuerSecurityOption2
>;
export declare function detachUserSessionIssuerSecurityOption2ToJSON(
  detachUserSessionIssuerSecurityOption2: DetachUserSessionIssuerSecurityOption2,
): string;
/** @internal */
export type DetachUserSessionIssuerSecurity$Outbound = {
  Option1?: DetachUserSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: DetachUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DetachUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  DetachUserSessionIssuerSecurity$Outbound,
  DetachUserSessionIssuerSecurity
>;
export declare function detachUserSessionIssuerSecurityToJSON(
  detachUserSessionIssuerSecurity: DetachUserSessionIssuerSecurity,
): string;
/** @internal */
export type DetachUserSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  AttachUserSessionIssuerForm: AttachUserSessionIssuerForm$Outbound;
};
/** @internal */
export declare const DetachUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  DetachUserSessionIssuerRequest$Outbound,
  DetachUserSessionIssuerRequest
>;
export declare function detachUserSessionIssuerRequestToJSON(
  detachUserSessionIssuerRequest: DetachUserSessionIssuerRequest,
): string;
//# sourceMappingURL=detachusersessionissuer.d.ts.map
