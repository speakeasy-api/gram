import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Change how the client authenticates at the issuer's token endpoint.
 */
export declare const UpdateRemoteSessionClientFormTokenEndpointAuthMethod: {
  readonly ClientSecretBasic: "client_secret_basic";
  readonly ClientSecretPost: "client_secret_post";
  readonly None: "none";
};
/**
 * Change how the client authenticates at the issuer's token endpoint.
 */
export type UpdateRemoteSessionClientFormTokenEndpointAuthMethod = ClosedEnum<
  typeof UpdateRemoteSessionClientFormTokenEndpointAuthMethod
>;
/**
 * Form for updating a remote_session_client. All non-id fields are optional patches.
 */
export type UpdateRemoteSessionClientForm = {
  /**
   * Replace the upstream OAuth audience sent for this client. Omit to leave unchanged.
   */
  audience?: string | undefined;
  /**
   * Rotate the client secret. Gram re-encrypts before persisting.
   */
  clientSecret?: string | undefined;
  /**
   * The remote_session_client id.
   */
  id: string;
  /**
   * Replace the explicit upstream OAuth scopes for this client. Omit to leave unchanged.
   */
  scope?: Array<string> | undefined;
  /**
   * Change how the client authenticates at the issuer's token endpoint.
   */
  tokenEndpointAuthMethod?:
    | UpdateRemoteSessionClientFormTokenEndpointAuthMethod
    | undefined;
};
/** @internal */
export declare const UpdateRemoteSessionClientFormTokenEndpointAuthMethod$outboundSchema: z.ZodMiniEnum<
  typeof UpdateRemoteSessionClientFormTokenEndpointAuthMethod
>;
/** @internal */
export type UpdateRemoteSessionClientForm$Outbound = {
  audience?: string | undefined;
  client_secret?: string | undefined;
  id: string;
  scope?: Array<string> | undefined;
  token_endpoint_auth_method?: string | undefined;
};
/** @internal */
export declare const UpdateRemoteSessionClientForm$outboundSchema: z.ZodMiniType<
  UpdateRemoteSessionClientForm$Outbound,
  UpdateRemoteSessionClientForm
>;
export declare function updateRemoteSessionClientFormToJSON(
  updateRemoteSessionClientForm: UpdateRemoteSessionClientForm,
): string;
//# sourceMappingURL=updateremotesessionclientform.d.ts.map
