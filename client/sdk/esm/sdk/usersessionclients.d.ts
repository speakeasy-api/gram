import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { UserSessionClient } from "../models/components/usersessionclient.js";
import {
  GetUserSessionClientRequest,
  GetUserSessionClientSecurity,
} from "../models/operations/getusersessionclient.js";
import {
  ListUserSessionClientsRequest,
  ListUserSessionClientsResponse,
  ListUserSessionClientsSecurity,
} from "../models/operations/listusersessionclients.js";
import {
  RevokeUserSessionClientRequest,
  RevokeUserSessionClientSecurity,
} from "../models/operations/revokeusersessionclient.js";
import { PageIterator } from "../types/operations.js";
export declare class UserSessionClients extends ClientSDK {
  /**
   * getUserSessionClient userSessionClients
   *
   * @remarks
   * Get a user_session_client by id.
   */
  get(
    request: GetUserSessionClientRequest,
    security?: GetUserSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UserSessionClient>;
  /**
   * listUserSessionClients userSessionClients
   *
   * @remarks
   * List user_session_clients in the caller's project.
   */
  list(
    request?: ListUserSessionClientsRequest | undefined,
    security?: ListUserSessionClientsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListUserSessionClientsResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * revokeUserSessionClient userSessionClients
   *
   * @remarks
   * Soft-delete a user_session_client. Future tokens minted for this client_id are rejected; existing live user_sessions keep working until they hit expires_at.
   */
  revoke(
    request: RevokeUserSessionClientRequest,
    security?: RevokeUserSessionClientSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
}
//# sourceMappingURL=usersessionclients.d.ts.map
