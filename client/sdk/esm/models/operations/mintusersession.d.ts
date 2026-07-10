import * as z from "zod/v4-mini";
import {
  MintUserSessionRequestBody,
  MintUserSessionRequestBody$Outbound,
} from "../components/mintusersessionrequestbody.js";
export type MintUserSessionSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type MintUserSessionRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  mintUserSessionRequestBody: MintUserSessionRequestBody;
};
/** @internal */
export type MintUserSessionSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const MintUserSessionSecurity$outboundSchema: z.ZodMiniType<
  MintUserSessionSecurity$Outbound,
  MintUserSessionSecurity
>;
export declare function mintUserSessionSecurityToJSON(
  mintUserSessionSecurity: MintUserSessionSecurity,
): string;
/** @internal */
export type MintUserSessionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  MintUserSessionRequestBody: MintUserSessionRequestBody$Outbound;
};
/** @internal */
export declare const MintUserSessionRequest$outboundSchema: z.ZodMiniType<
  MintUserSessionRequest$Outbound,
  MintUserSessionRequest
>;
export declare function mintUserSessionRequestToJSON(
  mintUserSessionRequest: MintUserSessionRequest,
): string;
//# sourceMappingURL=mintusersession.d.ts.map
