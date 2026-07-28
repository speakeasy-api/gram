import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { OrganizationIssuerDeletePreflight } from "../models/components/organizationissuerdeletepreflight.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import {
  CreateOrganizationRemoteSessionIssuerRequest,
  CreateOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/createorganizationremotesessionissuer.js";
import {
  DeleteOrganizationRemoteSessionIssuerRequest,
  DeleteOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/deleteorganizationremotesessionissuer.js";
import {
  GetOrganizationRemoteSessionIssuerRequest,
  GetOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/getorganizationremotesessionissuer.js";
import {
  GetOrganizationRemoteSessionIssuerDeletePreflightRequest,
  GetOrganizationRemoteSessionIssuerDeletePreflightSecurity,
} from "../models/operations/getorganizationremotesessionissuerdeletepreflight.js";
import {
  ListOrganizationRemoteSessionIssuersRequest,
  ListOrganizationRemoteSessionIssuersResponse,
  ListOrganizationRemoteSessionIssuersSecurity,
} from "../models/operations/listorganizationremotesessionissuers.js";
import {
  MoveOrganizationRemoteSessionIssuerRequest,
  MoveOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/moveorganizationremotesessionissuer.js";
import {
  UpdateOrganizationRemoteSessionIssuerRequest,
  UpdateOrganizationRemoteSessionIssuerSecurity,
} from "../models/operations/updateorganizationremotesessionissuer.js";
import { PageIterator } from "../types/operations.js";
export declare class OrganizationRemoteSessionIssuers extends ClientSDK {
  /**
   * createIssuer organizationRemoteSessionIssuers
   *
   * @remarks
   * Create a remote_session_issuer in the caller's organization. With no project_id the issuer is organization-level (project_id NULL, inherited by every project); with a project_id (which must belong to the organization) it is project-specific. Requires org:admin.
   */
  create(
    request: CreateOrganizationRemoteSessionIssuerRequest,
    security?: CreateOrganizationRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
  /**
   * deleteIssuer organizationRemoteSessionIssuers
   *
   * @remarks
   * Soft-delete any remote_session_issuer (organizational or project-specific) in the caller's organization. Blocked when any remote_session_clients still reference it. Requires org:admin.
   */
  delete(
    request: DeleteOrganizationRemoteSessionIssuerRequest,
    security?: DeleteOrganizationRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getIssuer organizationRemoteSessionIssuers
   *
   * @remarks
   * Get any remote_session_issuer (organizational or project-specific) in the caller's organization by id. Requires org:read.
   */
  get(
    request: GetOrganizationRemoteSessionIssuerRequest,
    security?: GetOrganizationRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
  /**
   * getIssuerDeletePreflight organizationRemoteSessionIssuers
   *
   * @remarks
   * Authoritative impact summary for deleting a remote_session_issuer: associated client count and affected MCP server names. Requires org:read.
   */
  getDeletePreflight(
    request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest,
    security?:
      | GetOrganizationRemoteSessionIssuerDeletePreflightSecurity
      | undefined,
    options?: RequestOptions,
  ): Promise<OrganizationIssuerDeletePreflight>;
  /**
   * listIssuers organizationRemoteSessionIssuers
   *
   * @remarks
   * List all remote_session_issuers in the caller's organization — organizational (project_id NULL) and project-specific — each with its associated client count and, for project-specific issuers, the owning project name. Requires org:read.
   */
  list(
    request?: ListOrganizationRemoteSessionIssuersRequest | undefined,
    security?: ListOrganizationRemoteSessionIssuersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListOrganizationRemoteSessionIssuersResponse,
      {
        cursor: string;
      }
    >
  >;
  /**
   * moveIssuer organizationRemoteSessionIssuers
   *
   * @remarks
   * Re-scope a remote_session_issuer in the caller's organization: provide a project_id (which must belong to the organization) to make it project-specific, or omit it to make it organization-level (project_id NULL, inherited by every project). Requires org:admin.
   */
  move(
    request: MoveOrganizationRemoteSessionIssuerRequest,
    security?: MoveOrganizationRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
  /**
   * updateIssuer organizationRemoteSessionIssuers
   *
   * @remarks
   * Update any remote_session_issuer (organizational or project-specific) in the caller's organization. Requires org:admin.
   */
  update(
    request: UpdateOrganizationRemoteSessionIssuerRequest,
    security?: UpdateOrganizationRemoteSessionIssuerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteSessionIssuer>;
}
//# sourceMappingURL=organizationremotesessionissuers.d.ts.map
