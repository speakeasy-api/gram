import * as z from "zod/v4-mini";
import {
  CreateGcpIamCredentialForm,
  CreateGcpIamCredentialForm$Outbound,
} from "../components/creategcpiamcredentialform.js";
export type CreateGcpIamCredentialSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateGcpIamCredentialRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createGcpIamCredentialForm: CreateGcpIamCredentialForm;
};
/** @internal */
export type CreateGcpIamCredentialSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateGcpIamCredentialSecurity$outboundSchema: z.ZodMiniType<
  CreateGcpIamCredentialSecurity$Outbound,
  CreateGcpIamCredentialSecurity
>;
export declare function createGcpIamCredentialSecurityToJSON(
  createGcpIamCredentialSecurity: CreateGcpIamCredentialSecurity,
): string;
/** @internal */
export type CreateGcpIamCredentialRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateGcpIamCredentialForm: CreateGcpIamCredentialForm$Outbound;
};
/** @internal */
export declare const CreateGcpIamCredentialRequest$outboundSchema: z.ZodMiniType<
  CreateGcpIamCredentialRequest$Outbound,
  CreateGcpIamCredentialRequest
>;
export declare function createGcpIamCredentialRequestToJSON(
  createGcpIamCredentialRequest: CreateGcpIamCredentialRequest,
): string;
//# sourceMappingURL=creategcpiamcredential.d.ts.map
