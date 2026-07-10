import * as z from "zod/v4-mini";
import {
  VerifyURLForm,
  VerifyURLForm$Outbound,
} from "../components/verifyurlform.js";
export type VerifyRemoteMcpURLSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type VerifyRemoteMcpURLSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type VerifyRemoteMcpURLSecurity = {
  option1?: VerifyRemoteMcpURLSecurityOption1 | undefined;
  option2?: VerifyRemoteMcpURLSecurityOption2 | undefined;
};
export type VerifyRemoteMcpURLRequest = {
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
  verifyURLForm: VerifyURLForm;
};
/** @internal */
export type VerifyRemoteMcpURLSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const VerifyRemoteMcpURLSecurityOption1$outboundSchema: z.ZodMiniType<
  VerifyRemoteMcpURLSecurityOption1$Outbound,
  VerifyRemoteMcpURLSecurityOption1
>;
export declare function verifyRemoteMcpURLSecurityOption1ToJSON(
  verifyRemoteMcpURLSecurityOption1: VerifyRemoteMcpURLSecurityOption1,
): string;
/** @internal */
export type VerifyRemoteMcpURLSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const VerifyRemoteMcpURLSecurityOption2$outboundSchema: z.ZodMiniType<
  VerifyRemoteMcpURLSecurityOption2$Outbound,
  VerifyRemoteMcpURLSecurityOption2
>;
export declare function verifyRemoteMcpURLSecurityOption2ToJSON(
  verifyRemoteMcpURLSecurityOption2: VerifyRemoteMcpURLSecurityOption2,
): string;
/** @internal */
export type VerifyRemoteMcpURLSecurity$Outbound = {
  Option1?: VerifyRemoteMcpURLSecurityOption1$Outbound | undefined;
  Option2?: VerifyRemoteMcpURLSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const VerifyRemoteMcpURLSecurity$outboundSchema: z.ZodMiniType<
  VerifyRemoteMcpURLSecurity$Outbound,
  VerifyRemoteMcpURLSecurity
>;
export declare function verifyRemoteMcpURLSecurityToJSON(
  verifyRemoteMcpURLSecurity: VerifyRemoteMcpURLSecurity,
): string;
/** @internal */
export type VerifyRemoteMcpURLRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  VerifyURLForm: VerifyURLForm$Outbound;
};
/** @internal */
export declare const VerifyRemoteMcpURLRequest$outboundSchema: z.ZodMiniType<
  VerifyRemoteMcpURLRequest$Outbound,
  VerifyRemoteMcpURLRequest
>;
export declare function verifyRemoteMcpURLRequestToJSON(
  verifyRemoteMcpURLRequest: VerifyRemoteMcpURLRequest,
): string;
//# sourceMappingURL=verifyremotemcpurl.d.ts.map
