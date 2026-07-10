import * as z from "zod/v4-mini";
import {
  SetUserSessionIssuerRequestBody,
  SetUserSessionIssuerRequestBody$Outbound,
} from "../components/setusersessionissuerrequestbody.js";
export type SetToolsetUserSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SetToolsetUserSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SetToolsetUserSessionIssuerSecurity = {
  option1?: SetToolsetUserSessionIssuerSecurityOption1 | undefined;
  option2?: SetToolsetUserSessionIssuerSecurityOption2 | undefined;
};
export type SetToolsetUserSessionIssuerRequest = {
  /**
   * The slug of the toolset to link
   */
  slug: string;
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
  setUserSessionIssuerRequestBody: SetUserSessionIssuerRequestBody;
};
/** @internal */
export type SetToolsetUserSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SetToolsetUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  SetToolsetUserSessionIssuerSecurityOption1$Outbound,
  SetToolsetUserSessionIssuerSecurityOption1
>;
export declare function setToolsetUserSessionIssuerSecurityOption1ToJSON(
  setToolsetUserSessionIssuerSecurityOption1: SetToolsetUserSessionIssuerSecurityOption1,
): string;
/** @internal */
export type SetToolsetUserSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SetToolsetUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  SetToolsetUserSessionIssuerSecurityOption2$Outbound,
  SetToolsetUserSessionIssuerSecurityOption2
>;
export declare function setToolsetUserSessionIssuerSecurityOption2ToJSON(
  setToolsetUserSessionIssuerSecurityOption2: SetToolsetUserSessionIssuerSecurityOption2,
): string;
/** @internal */
export type SetToolsetUserSessionIssuerSecurity$Outbound = {
  Option1?: SetToolsetUserSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: SetToolsetUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SetToolsetUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  SetToolsetUserSessionIssuerSecurity$Outbound,
  SetToolsetUserSessionIssuerSecurity
>;
export declare function setToolsetUserSessionIssuerSecurityToJSON(
  setToolsetUserSessionIssuerSecurity: SetToolsetUserSessionIssuerSecurity,
): string;
/** @internal */
export type SetToolsetUserSessionIssuerRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SetUserSessionIssuerRequestBody: SetUserSessionIssuerRequestBody$Outbound;
};
/** @internal */
export declare const SetToolsetUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  SetToolsetUserSessionIssuerRequest$Outbound,
  SetToolsetUserSessionIssuerRequest
>;
export declare function setToolsetUserSessionIssuerRequestToJSON(
  setToolsetUserSessionIssuerRequest: SetToolsetUserSessionIssuerRequest,
): string;
//# sourceMappingURL=settoolsetusersessionissuer.d.ts.map
