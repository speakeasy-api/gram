import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export declare const CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod: {
  readonly ClientSecretBasic: "client_secret_basic";
  readonly ClientSecretPost: "client_secret_post";
  readonly None: "none";
};
/**
 * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export type CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod =
  ClosedEnum<
    typeof CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod
  >;
/**
 * Form for an org admin to register a standalone remote_session_client under an existing issuer, with no user_session_issuer attachments.
 */
export type CreateOrganizationRemoteSessionClientForm = {
  /**
   * Optional upstream OAuth audience to send on the authorize redirect and token exchange.
   */
  audience?: string | undefined;
  /**
   * client_id supplied by the caller, e.g. from Dynamic Client Registration.
   */
  clientId: string;
  /**
   * Optional client_secret supplied by the caller. Gram encrypts before persisting; the plaintext is never returned.
   */
  clientSecret?: string | undefined;
  /**
   * Owning project id for the new client; the project must belong to the caller's organization. Omit to inherit a project-specific issuer's project, or to create an organization-level client (no project, attachable by every project) under an organization-level issuer.
   */
  projectId?: string | undefined;
  /**
   * The owning remote_session_issuer id; must belong to the caller's organization.
   */
  remoteSessionIssuerId: string;
  /**
   * Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.
   */
  scope?: Array<string> | undefined;
  /**
   * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
   */
  tokenEndpointAuthMethod?:
    | CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod
    | undefined;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod$outboundSchema: z.ZodMiniEnum<
  typeof CreateOrganizationRemoteSessionClientFormTokenEndpointAuthMethod
>;
/** @internal */
export type CreateOrganizationRemoteSessionClientForm$Outbound = {
  audience?: string | undefined;
  client_id: string;
  client_secret?: string | undefined;
  project_id?: string | undefined;
  remote_session_issuer_id: string;
  scope?: Array<string> | undefined;
  token_endpoint_auth_method?: string | undefined;
};
/** @internal */
export declare const CreateOrganizationRemoteSessionClientForm$outboundSchema: z.ZodMiniType<
  CreateOrganizationRemoteSessionClientForm$Outbound,
  CreateOrganizationRemoteSessionClientForm
>;
export declare function createOrganizationRemoteSessionClientFormToJSON(
  createOrganizationRemoteSessionClientForm: CreateOrganizationRemoteSessionClientForm,
): string;
//# sourceMappingURL=createorganizationremotesessionclientform.d.ts.map
