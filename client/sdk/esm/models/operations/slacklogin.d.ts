import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SlackLoginSecurity = {
  projectSlugQueryProjectSlug?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type SlackLoginRequest = {
  projectSlug: string;
  /**
   * The dashboard location to return too
   */
  returnUrl?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
export type SlackLoginResponse = {
  headers: {
    [k: string]: Array<string>;
  };
};
/** @internal */
export type SlackLoginSecurity$Outbound = {
  project_slug_query_project_slug?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SlackLoginSecurity$outboundSchema: z.ZodType<
  SlackLoginSecurity$Outbound,
  z.ZodTypeDef,
  SlackLoginSecurity
>;
export declare function slackLoginSecurityToJSON(
  slackLoginSecurity: SlackLoginSecurity,
): string;
/** @internal */
export type SlackLoginRequest$Outbound = {
  project_slug: string;
  return_url?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SlackLoginRequest$outboundSchema: z.ZodType<
  SlackLoginRequest$Outbound,
  z.ZodTypeDef,
  SlackLoginRequest
>;
export declare function slackLoginRequestToJSON(
  slackLoginRequest: SlackLoginRequest,
): string;
/** @internal */
export declare const SlackLoginResponse$inboundSchema: z.ZodType<
  SlackLoginResponse,
  z.ZodTypeDef,
  unknown
>;
export declare function slackLoginResponseFromJSON(
  jsonString: string,
): SafeParseResult<SlackLoginResponse, SDKValidationError>;
//# sourceMappingURL=slacklogin.d.ts.map
