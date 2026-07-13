import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { InfoResponseBody } from "../components/inforesponsebody.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SessionInfoSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type SessionInfoRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
export type SessionInfoResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: InfoResponseBody;
};
/** @internal */
export type SessionInfoSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SessionInfoSecurity$outboundSchema: z.ZodMiniType<
  SessionInfoSecurity$Outbound,
  SessionInfoSecurity
>;
export declare function sessionInfoSecurityToJSON(
  sessionInfoSecurity: SessionInfoSecurity,
): string;
/** @internal */
export type SessionInfoRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SessionInfoRequest$outboundSchema: z.ZodMiniType<
  SessionInfoRequest$Outbound,
  SessionInfoRequest
>;
export declare function sessionInfoRequestToJSON(
  sessionInfoRequest: SessionInfoRequest,
): string;
/** @internal */
export declare const SessionInfoResponse$inboundSchema: z.ZodMiniType<
  SessionInfoResponse,
  unknown
>;
export declare function sessionInfoResponseFromJSON(
  jsonString: string,
): SafeParseResult<SessionInfoResponse, SDKValidationError>;
//# sourceMappingURL=sessioninfo.d.ts.map
