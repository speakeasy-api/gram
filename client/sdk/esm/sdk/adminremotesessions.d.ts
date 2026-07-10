import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import {
  CreateGlobalRemoteSessionClientRequest,
  CreateGlobalRemoteSessionClientSecurity,
} from "../models/operations/createglobalremotesessionclient.js";
import {
  CreateGlobalRemoteSessionIssuerRequest,
  CreateGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/createglobalremotesessionissuer.js";
import {
  DeleteGlobalRemoteSessionClientRequest,
  DeleteGlobalRemoteSessionClientSecurity,
} from "../models/operations/deleteglobalremotesessionclient.js";
import {
  DeleteGlobalRemoteSessionIssuerRequest,
  DeleteGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/deleteglobalremotesessionissuer.js";
import {
  GetGlobalRemoteSessionClientRequest,
  GetGlobalRemoteSessionClientSecurity,
} from "../models/operations/getglobalremotesessionclient.js";
import {
  GetGlobalRemoteSessionIssuerRequest,
  GetGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/getglobalremotesessionissuer.js";
import {
  ListGlobalRemoteSessionClientsRequest,
  ListGlobalRemoteSessionClientsResponse,
  ListGlobalRemoteSessionClientsSecurity,
} from "../models/operations/listglobalremotesessionclients.js";
import {
  ListGlobalRemoteSessionIssuersRequest,
  ListGlobalRemoteSessionIssuersResponse,
  ListGlobalRemoteSessionIssuersSecurity,
} from "../models/operations/listglobalremotesessionissuers.js";
import {
  UpdateGlobalRemoteSessionClientRequest,
  UpdateGlobalRemoteSessionClientSecurity,
} from "../models/operations/updateglobalremotesessionclient.js";
import {
  UpdateGlobalRemoteSessionIssuerRequest,
  UpdateGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/updateglobalremotesessionissuer.js";
import { PageIterator } from "../types/operations.js";
export declare class AdminRemoteSessions extends ClientSDK {
  /**
   * createGlobalClient adminRemoteSessions
   *
   * @remarks
   * Register a global remote_session_client under an existing global remote_session_issuer. Caller supplies client_id and optional client_secret obtained out-of-band from the upstream issuer. Requires platform admin.
   */
  createGlobalClient(
    request: CreateGlobalRemoteSessionClientRequest,
    security?: CreateGlobalRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * createGlobalIssuer adminRemoteSessions
   *
   * @remarks
   * Create a global remote_session_issuer (project_id NULL, organization_id NULL). Requires platform admin.
   */
  createGlobalIssuer(
    request: CreateGlobalRemoteSessionIssuerRequest,
    security?: CreateGlobalRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
  /**
   * deleteGlobalClient adminRemoteSessions
   *
   * @remarks
   * Soft-delete a global remote_session_client. Cascades to the remote_sessions minted against it. Requires platform admin.
   */
  deleteGlobalClient(
    request: DeleteGlobalRemoteSessionClientRequest,
    security?: DeleteGlobalRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * deleteGlobalIssuer adminRemoteSessions
   *
   * @remarks
   * Soft-delete a global remote_session_issuer. Blocked when any global remote_session_clients still reference it. Requires platform admin.
   */
  deleteGlobalIssuer(
    request: DeleteGlobalRemoteSessionIssuerRequest,
    security?: DeleteGlobalRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getGlobalClient adminRemoteSessions
   *
   * @remarks
   * Get a global remote_session_client by id. Requires platform admin.
   */
  getGlobalClient(
    request: GetGlobalRemoteSessionClientRequest,
    security?: GetGlobalRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * getGlobalIssuer adminRemoteSessions
   *
   * @remarks
   * Get a global remote_session_issuer by id. Requires platform admin.
   */
  getGlobalIssuer(
    request: GetGlobalRemoteSessionIssuerRequest,
    security?: GetGlobalRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
  /**
   * listGlobalClients adminRemoteSessions
   *
   * @remarks
   * List the global remote_session_clients registered with a global remote_session_issuer. Requires platform admin.
   */
  listGlobalClients(
    request: ListGlobalRemoteSessionClientsRequest,
    security?: ListGlobalRemoteSessionClientsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListGlobalRemoteSessionClientsResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * listGlobalIssuers adminRemoteSessions
   *
   * @remarks
   * List global remote_session_issuers. Requires platform admin.
   */
  listGlobalIssuers(
    request?: ListGlobalRemoteSessionIssuersRequest | undefined,
    security?: ListGlobalRemoteSessionIssuersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListGlobalRemoteSessionIssuersResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * updateGlobalClient adminRemoteSessions
   *
   * @remarks
   * Rotate the client_secret or change non-issuer settings on a global remote_session_client. Requires platform admin.
   */
  updateGlobalClient(
    request: UpdateGlobalRemoteSessionClientRequest,
    security?: UpdateGlobalRemoteSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionClient>;
  /**
   * updateGlobalIssuer adminRemoteSessions
   *
   * @remarks
   * Update a global remote_session_issuer. Requires platform admin.
   */
  updateGlobalIssuer(
    request: UpdateGlobalRemoteSessionIssuerRequest,
    security?: UpdateGlobalRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
}
//# sourceMappingURL=adminremotesessions.d.ts.map
