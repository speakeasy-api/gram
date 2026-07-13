import * as z from "zod/v4-mini";
import {
  CreateDomainRequestBody,
  CreateDomainRequestBody$Outbound,
} from "../components/createdomainrequestbody.js";
export type RegisterDomainSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type RegisterDomainRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createDomainRequestBody: CreateDomainRequestBody;
};
/** @internal */
export type RegisterDomainSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RegisterDomainSecurity$outboundSchema: z.ZodMiniType<
  RegisterDomainSecurity$Outbound,
  RegisterDomainSecurity
>;
export declare function registerDomainSecurityToJSON(
  registerDomainSecurity: RegisterDomainSecurity,
): string;
/** @internal */
export type RegisterDomainRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateDomainRequestBody: CreateDomainRequestBody$Outbound;
};
/** @internal */
export declare const RegisterDomainRequest$outboundSchema: z.ZodMiniType<
  RegisterDomainRequest$Outbound,
  RegisterDomainRequest
>;
export declare function registerDomainRequestToJSON(
  registerDomainRequest: RegisterDomainRequest,
): string;
//# sourceMappingURL=registerdomain.d.ts.map
