import * as z from "zod/v4-mini";
import {
  UpdateRemoteSessionClientForm,
  UpdateRemoteSessionClientForm$Outbound,
} from "../components/updateremotesessionclientform.js";
export type UpdateGlobalRemoteSessionClientSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateGlobalRemoteSessionClientRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateRemoteSessionClientForm: UpdateRemoteSessionClientForm;
};
/** @internal */
export type UpdateGlobalRemoteSessionClientSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateGlobalRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  UpdateGlobalRemoteSessionClientSecurity$Outbound,
  UpdateGlobalRemoteSessionClientSecurity
>;
export declare function updateGlobalRemoteSessionClientSecurityToJSON(
  updateGlobalRemoteSessionClientSecurity: UpdateGlobalRemoteSessionClientSecurity,
): string;
/** @internal */
export type UpdateGlobalRemoteSessionClientRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  UpdateRemoteSessionClientForm: UpdateRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const UpdateGlobalRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  UpdateGlobalRemoteSessionClientRequest$Outbound,
  UpdateGlobalRemoteSessionClientRequest
>;
export declare function updateGlobalRemoteSessionClientRequestToJSON(
  updateGlobalRemoteSessionClientRequest: UpdateGlobalRemoteSessionClientRequest,
): string;
//# sourceMappingURL=updateglobalremotesessionclient.d.ts.map
