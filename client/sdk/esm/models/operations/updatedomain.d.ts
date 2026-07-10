import * as z from "zod/v4-mini";
import {
  UpdateDomainRequestBody,
  UpdateDomainRequestBody$Outbound,
} from "../components/updatedomainrequestbody.js";
export type UpdateDomainSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateDomainRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateDomainRequestBody: UpdateDomainRequestBody;
};
/** @internal */
export type UpdateDomainSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateDomainSecurity$outboundSchema: z.ZodMiniType<
  UpdateDomainSecurity$Outbound,
  UpdateDomainSecurity
>;
export declare function updateDomainSecurityToJSON(
  updateDomainSecurity: UpdateDomainSecurity,
): string;
/** @internal */
export type UpdateDomainRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  UpdateDomainRequestBody: UpdateDomainRequestBody$Outbound;
};
/** @internal */
export declare const UpdateDomainRequest$outboundSchema: z.ZodMiniType<
  UpdateDomainRequest$Outbound,
  UpdateDomainRequest
>;
export declare function updateDomainRequestToJSON(
  updateDomainRequest: UpdateDomainRequest,
): string;
//# sourceMappingURL=updatedomain.d.ts.map
