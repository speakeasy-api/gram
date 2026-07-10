import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RemoteSession } from "../models/components/remotesession.js";
import { RevokeAllRemoteSessionsResult } from "../models/components/revokeallremotesessionsresult.js";
import {
  ListOrganizationRemoteSessionClientSessionsRequest,
  ListOrganizationRemoteSessionClientSessionsResponse,
  ListOrganizationRemoteSessionClientSessionsSecurity,
} from "../models/operations/listorganizationremotesessionclientsessions.js";
import {
  RefreshOrganizationRemoteSessionRequest,
  RefreshOrganizationRemoteSessionSecurity,
} from "../models/operations/refreshorganizationremotesession.js";
import {
  RevokeAllOrganizationRemoteSessionClientSessionsRequest,
  RevokeAllOrganizationRemoteSessionClientSessionsSecurity,
} from "../models/operations/revokeallorganizationremotesessionclientsessions.js";
import {
  RevokeOrganizationRemoteSessionRequest,
  RevokeOrganizationRemoteSessionSecurity,
} from "../models/operations/revokeorganizationremotesession.js";
import { PageIterator } from "../types/operations.js";
export declare class OrganizationRemoteSessions extends ClientSDK {
  /**
   * listClientSessions organizationRemoteSessions
   *
   * @remarks
   * List the remote_sessions minted against a remote_session_client in the caller's organization. access_token_encrypted and refresh_token_encrypted are never returned. Requires org:read.
   */
  list(
    request: ListOrganizationRemoteSessionClientSessionsRequest,
    security?: ListOrganizationRemoteSessionClientSessionsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListOrganizationRemoteSessionClientSessionsResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * refreshSession organizationRemoteSessions
   *
   * @remarks
   * Force an upstream token refresh on a single remote_session in the caller's organization, regardless of current access-token expiry. Returns the updated remote_session so callers can reflect the new expiry without a refetch. Fails with a bad-request error when the session holds no refresh token. Requires org:admin.
   */
  refresh(
    request: RefreshOrganizationRemoteSessionRequest,
    security?: RefreshOrganizationRemoteSessionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSession>;
  /**
   * revokeSession organizationRemoteSessions
   *
   * @remarks
   * Revoke (soft-delete) a single remote_session in the caller's organization. Requires org:admin.
   */
  revoke(
    request: RevokeOrganizationRemoteSessionRequest,
    security?: RevokeOrganizationRemoteSessionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * revokeAllClientSessions organizationRemoteSessions
   *
   * @remarks
   * Revoke (soft-delete) all remote_sessions minted against a remote_session_client in the caller's organization. Requires org:admin.
   */
  revokeAll(
    request: RevokeAllOrganizationRemoteSessionClientSessionsRequest,
    security?:
      | RevokeAllOrganizationRemoteSessionClientSessionsSecurity
      | undefined,
    options?: RequestOptions,
  ): Promise<RevokeAllRemoteSessionsResult>;
}
//# sourceMappingURL=organizationremotesessions.d.ts.map
