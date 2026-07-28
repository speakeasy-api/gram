import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type LogoutSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type LogoutRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
export type LogoutResponse = {
  headers: {
    [k: string]: Array<string>;
  };
};
/** @internal */
export type LogoutSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const LogoutSecurity$outboundSchema: z.ZodMiniType<
  LogoutSecurity$Outbound,
  LogoutSecurity
>;
export declare function logoutSecurityToJSON(
  logoutSecurity: LogoutSecurity,
): string;
/** @internal */
export type LogoutRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const LogoutRequest$outboundSchema: z.ZodMiniType<
  LogoutRequest$Outbound,
  LogoutRequest
>;
export declare function logoutRequestToJSON(
  logoutRequest: LogoutRequest,
): string;
/** @internal */
export declare const LogoutResponse$inboundSchema: z.ZodMiniType<
  LogoutResponse,
  unknown
>;
export declare function logoutResponseFromJSON(
  jsonString: string,
): SafeParseResult<LogoutResponse, SDKValidationError>;
//# sourceMappingURL=logout.d.ts.map
