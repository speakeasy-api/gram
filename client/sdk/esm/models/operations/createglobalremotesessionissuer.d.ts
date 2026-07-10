import * as z from "zod/v4-mini";
import {
  CreateRemoteSessionIssuerForm,
  CreateRemoteSessionIssuerForm$Outbound,
} from "../components/createremotesessionissuerform.js";
export type CreateGlobalRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateGlobalRemoteSessionIssuerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createRemoteSessionIssuerForm: CreateRemoteSessionIssuerForm;
};
/** @internal */
export type CreateGlobalRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateGlobalRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  CreateGlobalRemoteSessionIssuerSecurity$Outbound,
  CreateGlobalRemoteSessionIssuerSecurity
>;
export declare function createGlobalRemoteSessionIssuerSecurityToJSON(
  createGlobalRemoteSessionIssuerSecurity: CreateGlobalRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type CreateGlobalRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateRemoteSessionIssuerForm: CreateRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const CreateGlobalRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  CreateGlobalRemoteSessionIssuerRequest$Outbound,
  CreateGlobalRemoteSessionIssuerRequest
>;
export declare function createGlobalRemoteSessionIssuerRequestToJSON(
  createGlobalRemoteSessionIssuerRequest: CreateGlobalRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=createglobalremotesessionissuer.d.ts.map
