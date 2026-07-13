import * as z from "zod/v4-mini";
import {
  UpdateRemoteSessionIssuerForm,
  UpdateRemoteSessionIssuerForm$Outbound,
} from "../components/updateremotesessionissuerform.js";
export type UpdateGlobalRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateGlobalRemoteSessionIssuerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm;
};
/** @internal */
export type UpdateGlobalRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateGlobalRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  UpdateGlobalRemoteSessionIssuerSecurity$Outbound,
  UpdateGlobalRemoteSessionIssuerSecurity
>;
export declare function updateGlobalRemoteSessionIssuerSecurityToJSON(
  updateGlobalRemoteSessionIssuerSecurity: UpdateGlobalRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type UpdateGlobalRemoteSessionIssuerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  UpdateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const UpdateGlobalRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  UpdateGlobalRemoteSessionIssuerRequest$Outbound,
  UpdateGlobalRemoteSessionIssuerRequest
>;
export declare function updateGlobalRemoteSessionIssuerRequestToJSON(
  updateGlobalRemoteSessionIssuerRequest: UpdateGlobalRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=updateglobalremotesessionissuer.d.ts.map
