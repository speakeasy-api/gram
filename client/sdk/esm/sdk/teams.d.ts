import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Teams extends ClientSDK {
  /**
   * cancelInvite teams
   *
   * @remarks
   * Cancel a pending invite.
   */
  cancelInvite(
    request: operations.CancelTeamInviteRequest,
    security?: operations.CancelTeamInviteSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getInviteInfo teams
   *
   * @remarks
   * Get information about a team invite by its token. Used to display invite details before accepting.
   */
  getInviteInfo(
    request: operations.GetTeamInviteInfoRequest,
    options?: RequestOptions,
  ): Promise<components.InviteInfoResult>;
  /**
   * inviteMember teams
   *
   * @remarks
   * Invite a new member to the organization.
   */
  inviteMember(
    request: operations.InviteTeamMemberRequest,
    security?: operations.InviteTeamMemberSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.InviteMemberResult>;
  /**
   * listInvites teams
   *
   * @remarks
   * List pending invites for an organization.
   */
  listInvites(
    request: operations.ListTeamInvitesRequest,
    security?: operations.ListTeamInvitesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ListInvitesResult>;
  /**
   * listMembers teams
   *
   * @remarks
   * List all members of an organization.
   */
  listMembers(
    request: operations.ListTeamMembersRequest,
    security?: operations.ListTeamMembersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ListMembersResult>;
  /**
   * removeMember teams
   *
   * @remarks
   * Remove a member from the organization.
   */
  removeMember(
    request: operations.RemoveTeamMemberRequest,
    security?: operations.RemoveTeamMemberSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * resendInvite teams
   *
   * @remarks
   * Resend an invite email.
   */
  resendInvite(
    request: operations.ResendTeamInviteRequest,
    security?: operations.ResendTeamInviteSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.ResendInviteResult>;
}
//# sourceMappingURL=teams.d.ts.map
