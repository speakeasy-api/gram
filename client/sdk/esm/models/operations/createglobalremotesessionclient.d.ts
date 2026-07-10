import * as z from "zod/v4-mini";
import {
  CreateGlobalRemoteSessionClientForm,
  CreateGlobalRemoteSessionClientForm$Outbound,
} from "../components/createglobalremotesessionclientform.js";
export type CreateGlobalRemoteSessionClientSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateGlobalRemoteSessionClientRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createGlobalRemoteSessionClientForm: CreateGlobalRemoteSessionClientForm;
};
/** @internal */
export type CreateGlobalRemoteSessionClientSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateGlobalRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  CreateGlobalRemoteSessionClientSecurity$Outbound,
  CreateGlobalRemoteSessionClientSecurity
>;
export declare function createGlobalRemoteSessionClientSecurityToJSON(
  createGlobalRemoteSessionClientSecurity: CreateGlobalRemoteSessionClientSecurity,
): string;
/** @internal */
export type CreateGlobalRemoteSessionClientRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateGlobalRemoteSessionClientForm: CreateGlobalRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const CreateGlobalRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  CreateGlobalRemoteSessionClientRequest$Outbound,
  CreateGlobalRemoteSessionClientRequest
>;
export declare function createGlobalRemoteSessionClientRequestToJSON(
  createGlobalRemoteSessionClientRequest: CreateGlobalRemoteSessionClientRequest,
): string;
//# sourceMappingURL=createglobalremotesessionclient.d.ts.map
