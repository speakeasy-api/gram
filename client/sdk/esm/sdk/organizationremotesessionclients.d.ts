import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListOrganizationMcpServersResult } from "../models/components/listorganizationmcpserversresult.js";
import { OrganizationClientDeletePreflight } from "../models/components/organizationclientdeletepreflight.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import {
  CreateCimdOrganizationRemoteSessionClientRequest,
  CreateCimdOrganizationRemoteSessionClientSecurity,
} from "../models/operations/createcimdorganizationremotesessionclient.js";
import {
  CreateOrganizationRemoteSessionClientRequest,
  CreateOrganizationRemoteSessionClientSecurity,
} from "../models/operations/createorganizationremotesessionclient.js";
import {
  DeleteOrganizationRemoteSessionClientRequest,
  DeleteOrganizationRemoteSessionClientSecurity,
} from "../models/operations/deleteorganizationremotesessionclient.js";
import {
  GetOrganizationRemoteSessionClientRequest,
  GetOrganizationRemoteSessionClientSecurity,
} from "../models/operations/getorganizationremotesessionclient.js";
import {
  GetOrganizationRemoteSessionClientDeletePreflightRequest,
  GetOrganizationRemoteSessionClientDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionclientdeletepreflight.js";
import {
  ListOrganizationRemoteSessionClientMcpServersRequest,
  ListOrganizationRemoteSessionClientMcpServersSecurity,
} from "../models/operations/listorganizationremotesessionclientmcpservers.js";
import {
  ListOrganizationRemoteSessionClientsRequest,
  ListOrganizationRemoteSessionClientsResponse,
  ListOrganizationRemoteSessionClientsSecurity,
} from "../models/operations/listorganizationremotesessionclients.js";
import {
  RemoveOrganizationRemoteSessionClientFromMcpServerRequest,
  RemoveOrganizationRemoteSessionClientFromMcpServerSecurity,
} from "../models/operations/removeorganizationremotesessionclientfrommcpserver.js";
import {
  UpdateOrganizationRemoteSessionClientRequest,
  UpdateOrganizationRemoteSessionClientSecurity,
} from "../models/operations/updateorganizationremotesessionclient.js";
import { PageIterator } from "../types/operations.js";
export declare class OrganizationRemoteSessionClients extends ClientSDK {
  /**
   * createClient organizationRemoteSessionClients
   *
   * @remarks
   * Register a standalone remote_session_client under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.
   */
  create(
    request: CreateOrganizationRemoteSessionClientRequest,
    security?: CreateOrganizationRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * createCimdClient organizationRemoteSessionClients
   *
   * @remarks
   * Register a standalone remote_session_client in Client ID Metadata Document (CIMD) mode under an existing remote_session_issuer in the caller's organization, with no user_session_issuer attachments. Gram generates the client_id and hosts the metadata document; the issuer must advertise client_id_metadata_document_supported. The client is project-scoped: it inherits a project-specific issuer's project, or the caller names a project (which must belong to the organization) when the issuer is organization-level. Requires org:admin.
   */
  createCimd(
    request: CreateCimdOrganizationRemoteSessionClientRequest,
    security?: CreateCimdOrganizationRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * deleteClient organizationRemoteSessionClients
   *
   * @remarks
   * Soft-delete a remote_session_client in the caller's organization. Cascades to the remote_sessions minted against it. Requires org:admin.
   */
  delete(
    request: DeleteOrganizationRemoteSessionClientRequest,
    security?: DeleteOrganizationRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getClient organizationRemoteSessionClients
   *
   * @remarks
   * Get a remote_session_client in the caller's organization by id. Requires org:read.
   */
  get(
    request: GetOrganizationRemoteSessionClientRequest,
    security?: GetOrganizationRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * getClientDeletePreflight organizationRemoteSessionClients
   *
   * @remarks
   * Authoritative impact summary for deleting a remote_session_client: associated session count and affected MCP server names. Requires org:read.
   */
  getDeletePreflight(
    request: GetOrganizationRemoteSessionClientDeletePreflightRequest,
    security?:
      | GetOrganizationRemoteSessionClientDeletePreflightSecurity
      | undefined,
    options?: RequestOptions,
  ): Promise<OrganizationClientDeletePreflight>;
  /**
   * listClients organizationRemoteSessionClients
   *
   * @remarks
   * List the remote_session_clients registered with a given issuer in the caller's organization, each with its MCP server attachment count. Requires org:read.
   */
  list(
    request: ListOrganizationRemoteSessionClientsRequest,
    security?: ListOrganizationRemoteSessionClientsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListOrganizationRemoteSessionClientsResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * listClientMcpServers organizationRemoteSessionClients
   *
   * @remarks
   * List the MCP servers a remote_session_client is attached to (resolved through user_session_issuers) in the caller's organization. Requires org:read.
   */
  listMcpServers(
    request: ListOrganizationRemoteSessionClientMcpServersRequest,
    security?:
      | ListOrganizationRemoteSessionClientMcpServersSecurity
      | undefined,
    options?: RequestOptions,
  ): Promise<ListOrganizationMcpServersResult>;
  /**
   * removeClientFromMcpServer organizationRemoteSessionClients
   *
   * @remarks
   * Detach a remote_session_client from an MCP server (clears the MCP server's user_session_issuer link) in the caller's organization. Requires org:admin.
   */
  removeFromMcpServer(
    request: RemoveOrganizationRemoteSessionClientFromMcpServerRequest,
    security?:
      | RemoveOrganizationRemoteSessionClientFromMcpServerSecurity
      | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * updateClient organizationRemoteSessionClients
   *
   * @remarks
   * Update a remote_session_client's non-secret fields in the caller's organization. Requires org:admin.
   */
  update(
    request: UpdateOrganizationRemoteSessionClientRequest,
    security?: UpdateOrganizationRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
}
//# sourceMappingURL=organizationremotesessionclients.d.ts.map
