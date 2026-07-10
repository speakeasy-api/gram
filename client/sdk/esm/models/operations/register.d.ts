import * as z from "zod/v4-mini";
import {
  RegisterRequestBody,
  RegisterRequestBody$Outbound,
} from "../components/registerrequestbody.js";
export type RegisterSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type RegisterRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  registerRequestBody: RegisterRequestBody;
};
/** @internal */
export type RegisterSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RegisterSecurity$outboundSchema: z.ZodMiniType<
  RegisterSecurity$Outbound,
  RegisterSecurity
>;
export declare function registerSecurityToJSON(
  registerSecurity: RegisterSecurity,
): string;
/** @internal */
export type RegisterRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  RegisterRequestBody: RegisterRequestBody$Outbound;
};
/** @internal */
export declare const RegisterRequest$outboundSchema: z.ZodMiniType<
  RegisterRequest$Outbound,
  RegisterRequest
>;
export declare function registerRequestToJSON(
  registerRequest: RegisterRequest,
): string;
//# sourceMappingURL=register.d.ts.map
