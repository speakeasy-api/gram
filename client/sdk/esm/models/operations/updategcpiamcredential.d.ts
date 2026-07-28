import * as z from "zod/v4-mini";
import {
  UpdateGcpIamCredentialRequestBody,
  UpdateGcpIamCredentialRequestBody$Outbound,
} from "../components/updategcpiamcredentialrequestbody.js";
export type UpdateGcpIamCredentialSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateGcpIamCredentialRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateGcpIamCredentialRequestBody: UpdateGcpIamCredentialRequestBody;
};
/** @internal */
export type UpdateGcpIamCredentialSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateGcpIamCredentialSecurity$outboundSchema: z.ZodMiniType<
  UpdateGcpIamCredentialSecurity$Outbound,
  UpdateGcpIamCredentialSecurity
>;
export declare function updateGcpIamCredentialSecurityToJSON(
  updateGcpIamCredentialSecurity: UpdateGcpIamCredentialSecurity,
): string;
/** @internal */
export type UpdateGcpIamCredentialRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  UpdateGcpIamCredentialRequestBody: UpdateGcpIamCredentialRequestBody$Outbound;
};
/** @internal */
export declare const UpdateGcpIamCredentialRequest$outboundSchema: z.ZodMiniType<
  UpdateGcpIamCredentialRequest$Outbound,
  UpdateGcpIamCredentialRequest
>;
export declare function updateGcpIamCredentialRequestToJSON(
  updateGcpIamCredentialRequest: UpdateGcpIamCredentialRequest,
): string;
//# sourceMappingURL=updategcpiamcredential.d.ts.map
